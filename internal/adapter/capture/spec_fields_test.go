package capture

// Byte-level fixtures for the Destiny Board messages (feature 011), mirroring the
// live dump layouts (E:154 snapshot, E:153 delta over 1914 samples, E:152 done).

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/photon"
)

func TestAchievementSnapshotFromBytes(t *testing.T) {
	params := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(73486)},
		{Key: 1, Type: photon.TypeArray | photon.TypeShort, Val: []int16{668, 22, 172}},
		{Key: 2, Type: photon.TypeArray | photon.TypeByte, Val: []byte{40, 22, 5}},
		{Key: 3, Type: photon.TypeArray | photon.TypeFloat, Val: []float32{0.238, 0.845, 1.5}}, // 1.5 clamps to 1
		{Key: 4, Type: photon.TypeArray | photon.TypeString, Val: []string{"[[433866458]]", "[[100]]", "bad"}},
		{Key: 252, Type: photon.TypeShort, Val: int16(154)},
	})
	if self, ok := SpecSelf(params); !ok || self != 73486 {
		t.Fatalf("self = %d/%v", self, ok)
	}
	nodes, ok := AchievementSnapshot(params)
	if !ok || len(nodes) != 3 {
		t.Fatalf("snapshot → %d/%v", len(nodes), ok)
	}
	if nodes[0].ID != 668 || nodes[0].Level != 40 || nodes[0].Fame != 433866458 {
		t.Fatalf("node0 wrong: %+v", nodes[0])
	}
	if nodes[2].Progress != 1 {
		t.Fatalf("progress must clamp to 1: %+v", nodes[2])
	}
	if nodes[2].Fame != 0 { // "bad" → 0, row survives
		t.Fatalf("bad fame must be 0: %+v", nodes[2])
	}

	// Short parallel arrays: missing fields stay zero, row still built.
	params = decodeEvent(t, []photon.Field{
		{Key: 1, Type: photon.TypeArray | photon.TypeShort, Val: []int16{1, 2, 3}},
		{Key: 2, Type: photon.TypeArray | photon.TypeByte, Val: []byte{5}}, // only one level
		{Key: 252, Type: photon.TypeShort, Val: int16(154)},
	})
	nodes, ok = AchievementSnapshot(params)
	if !ok || len(nodes) != 3 || nodes[0].Level != 5 || nodes[1].HasLevel {
		t.Fatalf("short-array handling wrong: %+v", nodes)
	}

	// Hostile: empty ids reject; oversized truncates via cap in the board, but the
	// extractor rejects an id array beyond the cap outright.
	if _, ok := AchievementSnapshot(map[byte]interface{}{1: []int16{}}); ok {
		t.Fatal("empty ids must be rejected")
	}
	huge := make([]int16, maxSpecNodes+1)
	if _, ok := AchievementSnapshot(map[byte]interface{}{1: huge}); ok {
		t.Fatal("oversized ids must be rejected")
	}
}

func TestAchievementDeltaFromBytes(t *testing.T) {
	// With level.
	params := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(73486)},
		{Key: 1, Type: photon.TypeInteger, Val: int32(668)},
		{Key: 2, Type: photon.TypeInteger, Val: int32(22)},
		{Key: 3, Type: photon.TypeFloat, Val: float32(0.238)},
		{Key: 4, Type: photon.TypeString, Val: "[[433866458]]"},
		{Key: 252, Type: photon.TypeShort, Val: int16(153)},
	})
	u, ok := AchievementDelta(params)
	if !ok || u.ID != 668 || !u.HasLevel || u.Level != 22 || u.Fame != 433866458 {
		t.Fatalf("delta wrong: %+v ok=%v", u, ok)
	}

	// Without level (k2 absent) — the live-confirmed optional case.
	params = decodeEvent(t, []photon.Field{
		{Key: 1, Type: photon.TypeInteger, Val: int32(670)},
		{Key: 3, Type: photon.TypeFloat, Val: float32(0.845)},
		{Key: 4, Type: photon.TypeString, Val: "[[433866458]]"},
		{Key: 252, Type: photon.TypeShort, Val: int16(153)},
	})
	u, ok = AchievementDelta(params)
	if !ok || u.HasLevel {
		t.Fatalf("absent level must yield HasLevel=false: %+v", u)
	}
}

func TestAchievementDoneFromBytes(t *testing.T) {
	params := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(76685)},
		{Key: 1, Type: photon.TypeInteger, Val: int32(116)},
		{Key: 2, Type: photon.TypeInteger, Val: int32(11)},
		{Key: 252, Type: photon.TypeShort, Val: int16(152)},
	})
	id, level, ok := AchievementDone(params)
	if !ok || id != 116 || level != 11 {
		t.Fatalf("done → id=%d level=%d ok=%v", id, level, ok)
	}
	if _, _, ok := AchievementDone(map[byte]interface{}{1: int32(1)}); ok {
		t.Fatal("missing level must be not-ok")
	}
}

// board260 builds a full-size E:1 k3 level array (>= minBoardNodes) with per-index
// overrides, plus the matching k2 id list and k4 flag array.
func board260(overrides map[int]int) (ids []int16, levels []uint8, flags []bool) {
	n := minBoardNodes + 4
	ids = make([]int16, n)
	levels = make([]uint8, n)
	flags = make([]bool, 133)
	for i := 0; i < n; i++ {
		ids[i] = int16(i + 6)
		if v, ok := overrides[i]; ok {
			levels[i] = uint8(v)
		}
	}
	return
}

// E:1 board (012): k2 ids + k3 levels direct; k2-less warm login; chat-E:1 reject.
func TestAchievementFullBoard(t *testing.T) {
	ids, levels, flags := board260(map[int]int{0: 10, 15: 100, 40: 55})
	// Cold login: k2 present, same length as k3.
	fb, ok := AchievementFullBoard(map[byte]interface{}{0: int64(76685), 2: ids, 3: levels, 4: flags})
	if !ok || !fb.IdsFromWire || len(fb.Ids) != len(levels) || fb.Levels[15] != 100 {
		t.Fatalf("cold-login decode wrong: ok=%v fromWire=%v", ok, fb.IdsFromWire)
	}
	// Warm login: k2 absent → Ids nil, Levels present.
	fb, ok = AchievementFullBoard(map[byte]interface{}{0: int64(76685), 3: levels, 4: flags})
	if !ok || fb.IdsFromWire || fb.Ids != nil || len(fb.Levels) != len(levels) {
		t.Fatalf("warm-login decode wrong: %+v ok=%v", fb, ok)
	}
	// Too-short level array (below minBoardNodes) → rejected (truncated/hostile packet).
	if _, ok := AchievementFullBoard(map[byte]interface{}{0: int64(1), 3: []uint8{1, 2, 3}, 4: flags}); ok {
		t.Fatal("a k3 shorter than the board must be rejected")
	}
	// Missing k4 flag array → not a real board → rejected.
	if _, ok := AchievementFullBoard(map[byte]interface{}{0: int64(1), 3: levels}); ok {
		t.Fatal("E:1 without the k4 []bool flag array must be rejected")
	}
	// Chat-settings E:1: k0 []string → rejected.
	if _, ok := AchievementFullBoard(map[byte]interface{}{0: []string{"x"}, 3: levels, 4: flags}); ok {
		t.Fatal("chat-settings E:1 (k0 []string) must be rejected")
	}
}
