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
