package app

// Destiny Board pipeline goldens (feature 011) — contract rules 1-8 through the
// REAL dispatch path with wire-shaped params (dump layouts).

import (
	"math"
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

const specSelf = 73486

// setSelf primes the pipeline's self identity (the spec handlers self-filter).
func (p *Pipeline) setSelfForTest(id int) { p.selfObjID = id; p.specReplacePending = true }

func snapshotParams(self int, ids []int16, levels []byte, progress []float32, fames []string) map[byte]interface{} {
	return map[byte]interface{}{
		0: int32(self), 1: ids, 2: levels, 3: progress, 4: fames, 252: int16(154),
	}
}

func deltaParams(self, id int, level *int, progress float32, fame string) map[byte]interface{} {
	m := map[byte]interface{}{0: int32(self), 1: int32(id), 3: progress, 4: fame, 252: int16(153)}
	if level != nil {
		m[2] = int32(*level)
	}
	return m
}

func specOf(svc interface{ Spec() model.CharacterSpec }) model.CharacterSpec { return svc.Spec() }

// Rules 1/2/5: snapshot fills the Spec view with resolved names + fallback.
func TestSpecSnapshotFills(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf,
		[]int16{22, 888}, []byte{22, 3}, []float32{0.5, 0.1}, []string{"[[100]]", "[[9]]"}))

	sp := specOf(svc)
	if sp.NodeCount != 2 || sp.TotalFame != 109 {
		t.Fatalf("totals wrong: %+v", sp)
	}
	// Node 22 resolves (testSpecNames), 888 falls back to "Node #888".
	byIdx := map[int]model.MasteryLevel{}
	for _, m := range sp.Masteries {
		byIdx[m.Index] = m
	}
	if byIdx[22].Name != "Combat Axes" || byIdx[22].Level != 22 || math.Abs(byIdx[22].Progress-0.5) > 1e-6 {
		t.Fatalf("node 22 wrong: %+v", byIdx[22])
	}
	if byIdx[888].Name != "Node #888" {
		t.Fatalf("unknown id must fall back: %+v", byIdx[888])
	}
}

// Rule 1: a second snapshot REPLACES (delta residue can't survive).
func TestSpecSnapshotReplaces(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{22, 30}, []byte{5, 5}, nil, nil))
	lvl := 9
	p.dispatch(probe.KindEvent, 153, deltaParams(specSelf, 88, &lvl, 0.2, "[[1]]")) // node 88 appears
	p.specReplacePending = true // a fresh Join re-sends the whole board
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{22}, []byte{6}, nil, nil))
	sp := specOf(svc)
	if sp.NodeCount != 1 || sp.Masteries[0].Index != 22 || sp.Masteries[0].Level != 6 {
		t.Fatalf("snapshot must REPLACE, got %+v", sp)
	}
}

// Rule 2: foreign self is ignored on all three messages.
func TestSpecSelfFilter(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	p.dispatch(probe.KindEvent, 154, snapshotParams(999999, []int16{1}, []byte{1}, nil, nil))
	if specOf(svc).NodeCount != 0 {
		t.Fatal("foreign snapshot must be ignored")
	}
}

// Rules 3/4: delta upsert (level optional) + completion; then snapshot reconciles.
func TestSpecDeltaAndDone(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{22}, []byte{5}, []float32{0.2}, nil))

	// Level-less delta keeps the level, updates progress.
	p.dispatch(probe.KindEvent, 153, deltaParams(specSelf, 22, nil, 0.6, "[[50]]"))
	if m := specOf(svc).Masteries[0]; m.Level != 5 || math.Abs(m.Progress-0.6) > 1e-6 || m.Fame != 50 {
		t.Fatalf("level-less delta wrong: %+v", m)
	}
	// Completion: level up, progress zero.
	p.dispatch(probe.KindEvent, 152, map[byte]interface{}{0: int32(specSelf), 1: int32(22), 2: int32(6), 252: int16(152)})
	if m := specOf(svc).Masteries[0]; m.Level != 6 || m.Progress != 0 {
		t.Fatalf("completion wrong: %+v", m)
	}
	// Delta for an unknown node creates a row; the next snapshot reconciles it away.
	lvl := 1
	p.dispatch(probe.KindEvent, 153, deltaParams(specSelf, 77, &lvl, 0.3, "[[7]]"))
	if specOf(svc).NodeCount != 2 {
		t.Fatal("unknown-node delta must create a row")
	}
	p.specReplacePending = true // Join boundary → the snapshot reconciles
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{22}, []byte{6}, nil, nil))
	if specOf(svc).NodeCount != 1 {
		t.Fatal("snapshot must reconcile away the phantom node")
	}
}

// Rule 6: hostile shapes don't disturb an existing view.
func TestSpecHostileIgnored(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{22}, []byte{5}, nil, nil))
	before := specOf(svc).NodeCount
	p.dispatch(probe.KindEvent, 154, map[byte]interface{}{0: int32(specSelf), 1: []int16{}, 252: int16(154)}) // empty ids
	if specOf(svc).NodeCount != before {
		t.Fatal("empty-ids snapshot must not change the view")
	}
}

// Live 2026-07-05: the board arrives as SEVERAL E:154 packets per Join (75+75+36);
// only the first of a burst replaces, the rest merge — else only the last survives.
func TestSpecMultiPacketBurstMerges(t *testing.T) {
	svc, p := newGlue(t)
	p.updateSelf(map[byte]interface{}{0: int32(specSelf), 2: "Hero"}) // arms replace
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{1, 2}, []byte{5, 5}, nil, nil))
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{3, 4}, []byte{5, 5}, nil, nil))
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{5}, []byte{5}, nil, nil))
	if n := specOf(svc).NodeCount; n != 5 {
		t.Fatalf("multi-packet burst must merge to 5, got %d", n)
	}
	// A fresh Join replaces the whole thing.
	p.updateSelf(map[byte]interface{}{0: int32(specSelf), 2: "Hero"})
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{9}, []byte{5}, nil, nil))
	if n := specOf(svc).NodeCount; n != 1 {
		t.Fatalf("new Join must replace, got %d", n)
	}
}

// E:155 = full unlocked list (in-progress ∪ maxed). Ids unlocked but absent from
// the in-progress snapshot are MAXED (level 100) — research-confirmed 2026-07-05.
func TestSpecUnlockedMarksMaxed(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	// In-progress snapshot: node 22 at level 30.
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{22}, []byte{30}, []float32{0.5}, nil))
	// E:155 unlocked set includes 22 (in-progress) + 999 (maxed, not in snapshot).
	p.dispatch(probe.KindEvent, 155, map[byte]interface{}{0: int32(specSelf), 1: []int16{22, 999}, 252: int16(155)})
	byIdx := map[int]model.MasteryLevel{}
	for _, m := range specOf(svc).Masteries {
		byIdx[m.Index] = m
	}
	if byIdx[22].Level != 30 {
		t.Fatalf("in-progress node must keep its level: %+v", byIdx[22])
	}
	if byIdx[999].Level != 100 || !byIdx[999].Touched {
		t.Fatalf("unlocked-but-not-in-progress node must be maxed: %+v", byIdx[999])
	}
	// Order independence: a later E:154 that includes 999 with a real level wins.
	p.specReplacePending = true
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{999}, []byte{40}, nil, nil))
	for _, m := range specOf(svc).Masteries {
		if m.Index == 999 && m.Level != 40 {
			t.Fatalf("in-progress must override maxed guess: %+v", m)
		}
	}
}

// Startup seed order: the persisted unlocked set arrives BEFORE any E:154. Nothing
// may be classified maxed until a snapshot separates in-progress from finished —
// live regression: everything with fame showed level 100.
func TestSeedBeforeSnapshotMarksNothingMaxed(t *testing.T) {
	svc, p := newGlue(t)
	p.SeedSpecUnlocked([]int{22, 999}) // full unlocked set: 22 in-progress, 999 maxed
	for _, m := range specOf(svc).Masteries {
		if m.Level == 100 {
			t.Fatalf("maxed classification before any snapshot: %+v", m)
		}
	}
	if specOf(svc).Complete {
		t.Fatal("complete must require BOTH the unlocked set and a snapshot")
	}
	// Snapshot arrives: 22 in-progress at 30 → 999 (unlocked, absent) becomes maxed.
	p.setSelfForTest(specSelf)
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{22}, []byte{30}, nil, nil))
	byIdx := map[int]model.MasteryLevel{}
	for _, m := range specOf(svc).Masteries {
		byIdx[m.Index] = m
	}
	if byIdx[22].Level != 30 || byIdx[999].Level != 100 {
		t.Fatalf("post-snapshot classification wrong: 22=%+v 999=%+v", byIdx[22], byIdx[999])
	}
	if !specOf(svc).Complete {
		t.Fatal("complete once both known")
	}
}

// Long-idle maxed nodes never appear in E:154/E:155 (live-proven). Any E:152/E:153
// showing level >=100 must permanently join the unlocked (maxed) set.
func TestLevel100ObservationsJoinMaxedSet(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{22}, []byte{30}, nil, nil))
	// Elite tick on a long-idle maxed node (999): E:153 with level 101.
	lvl := 101
	p.dispatch(probe.KindEvent, 153, deltaParams(specSelf, 999, &lvl, 0.1, "[[5]]"))
	// Next zone join re-sends only in-progress (22) — 999 must STAY maxed.
	p.updateSelf(map[byte]interface{}{0: int32(specSelf), 2: "Hero"})
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{22}, []byte{31}, nil, nil))
	for _, m := range specOf(svc).Masteries {
		if m.Index == 999 && m.Level < 100 {
			t.Fatalf("elite-observed node lost maxed status after snapshot: %+v", m)
		}
	}
}

// Review A #1: E:155 is an INCOMPLETE unlocked list; a completion-triggered E:155
// must MERGE (not replace) so seeded/observed long-idle maxed nodes survive.
func TestSpecUnlockedMergesNeverWipes(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	// Seed a persisted maxed node 999 (a long-idle robe absent from E:155).
	p.SeedSpecUnlocked([]int{999})
	// A snapshot so classification runs; 22 in-progress.
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{22}, []byte{30}, nil, nil))
	// A completion fires E:155 WITHOUT 999 (E:155 omits idle maxed).
	p.dispatch(probe.KindEvent, 155, map[byte]interface{}{0: int32(specSelf), 1: []int16{22}, 252: int16(155)})
	found999 := false
	for _, m := range specOf(svc).Masteries {
		if m.Index == 999 {
			found999 = m.Level == 100
		}
	}
	if !found999 {
		t.Fatal("E:155 wiped the seeded idle-maxed node 999 — must merge, not replace")
	}
}

// e1Board builds a full-size E:1 (>= minBoardNodes) with the given (position→id) order
// and per-position levels; the k2 id list is included when withIDs is true (cold login),
// omitted for a warm login. Positions past the overrides are unique filler ids at level 0.
func e1Board(self int, withIDs bool, ids []int16, levels []uint8) map[byte]interface{} {
	n := 260
	fullIDs := make([]int16, n)
	fullLvls := make([]uint8, n)
	flags := make([]bool, 133)
	for i := 0; i < n; i++ {
		fullIDs[i] = int16(400 + i) // filler ids, level 0 (not installed)
	}
	copy(fullIDs, ids)
	copy(fullLvls, levels)
	params := map[byte]interface{}{0: int32(self), 3: fullLvls, 4: flags, 252: int16(1)}
	if withIDs {
		params[2] = fullIDs
	}
	return params
}

// E:1 (012) is the COMPLETE board authority: k2 ids + k3 levels decode directly, so
// maxed nodes show at login with no grind.
func TestE1BoardAuthority(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	// Cold login E:1: id 22 maxed, id 6 in-progress (rest filler at level 0).
	p.dispatch(probe.KindEvent, 1, e1Board(specSelf, true, []int16{6, 22}, []uint8{10, 100}))
	sp := specOf(svc)
	if !sp.Complete {
		t.Fatal("E:1 board must mark the spec Complete (no banner)")
	}
	byIdx := map[int]model.MasteryLevel{}
	for _, m := range sp.Masteries {
		byIdx[m.Index] = m
	}
	if byIdx[22].Level != 100 {
		t.Fatalf("E:1 maxed node must show level 100: %+v", byIdx[22])
	}
	if byIdx[6].Level != 10 {
		t.Fatalf("E:1 in-progress level wrong: %+v", byIdx[6])
	}
	// Only TOUCHED nodes (level > 0) count — the hundreds of level-0 board nodes must
	// NOT inflate NodeCount (finding: touched-only invariant).
	if sp.NodeCount != 2 {
		t.Fatalf("NodeCount must be touched-only (2), got %d", sp.NodeCount)
	}
}

// Warm login E:1 (k2 absent) decodes via the enumeration learned from a prior cold
// login — same length required, or it falls back (FR-004).
func TestE1WarmLoginUsesEnum(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	// Cold login learns the full order.
	p.dispatch(probe.KindEvent, 1, e1Board(specSelf, true, []int16{6, 22}, []uint8{5, 5}))
	// Warm login: NO k2, k3 position-indexed by the learned order, SAME length.
	p.dispatch(probe.KindEvent, 1, e1Board(specSelf, false, nil, []uint8{11, 100}))
	byIdx := map[int]model.MasteryLevel{}
	for _, m := range specOf(svc).Masteries {
		byIdx[m.Index] = m
	}
	if byIdx[22].Level != 100 || byIdx[6].Level != 11 {
		t.Fatalf("warm-login enum decode wrong: 22=%+v 6=%+v", byIdx[22], byIdx[6])
	}
}

// A warm E:1 whose length differs from the learned enum is a different board version →
// refused, board untouched (FR-004: never show a wrong node).
func TestE1WarmLengthMismatchIgnored(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	p.dispatch(probe.KindEvent, 1, e1Board(specSelf, true, []int16{6, 22}, []uint8{5, 100}))
	before := specOf(svc).NodeCount
	// Warm login with a DIFFERENT-length k3 (still >= minBoardNodes) → must be ignored.
	flags := make([]bool, 133)
	longLvls := make([]uint8, 300)
	longLvls[0] = 99
	p.dispatch(probe.KindEvent, 1, map[byte]interface{}{0: int32(specSelf), 3: longLvls, 4: flags, 252: int16(1)})
	if got := specOf(svc).NodeCount; got != before {
		t.Fatalf("mismatched-length warm E:1 must not change the board: before=%d after=%d", before, got)
	}
}

// The chat-settings E:1 (k0 []string) shares event code 1 — it must be rejected.
func TestE1ChatSettingsRejected(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	p.dispatch(probe.KindEvent, 1, map[byte]interface{}{
		0: []string{"@CHAT_TAB_GENERAL"}, 1: []int16{1, 2}, 252: int16(1),
	})
	if specOf(svc).NodeCount != 0 || specOf(svc).Complete {
		t.Fatal("chat-settings E:1 must not touch the board")
	}
}

// E:1 absent → the 011 path (E:154 + E:155 + banner) is untouched.
func TestE1AbsentFallbackTo011(t *testing.T) {
	svc, p := newGlue(t)
	p.setSelfForTest(specSelf)
	p.dispatch(probe.KindEvent, 154, snapshotParams(specSelf, []int16{22}, []byte{30}, nil, nil))
	sp := specOf(svc)
	if m := func() model.MasteryLevel {
		for _, x := range sp.Masteries {
			if x.Index == 22 {
				return x
			}
		}
		return model.MasteryLevel{}
	}(); m.Level != 30 {
		t.Fatalf("011 E:154 path must still work without E:1: %+v", m)
	}
	if sp.Complete {
		t.Fatal("without E:1 or E:155, Complete must stay false (011 banner)")
	}
}
