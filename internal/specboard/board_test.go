package specboard

import "testing"

func TestReplaceAllIsAuthority(t *testing.T) {
	b := New()
	b.ReplaceAll([]Node{{ID: 1, Level: 5, Progress: 0.5, Fame: 100}, {ID: 2, Level: 3}})
	if n, fame := b.Totals(); n != 2 || fame != 100 {
		t.Fatalf("totals = %d/%d, want 2/100", n, fame)
	}
	// A delta then a fresh snapshot that excludes it → the delta cannot survive.
	b.Apply(Node{ID: 9, Level: 1}, true, true, true)
	b.ReplaceAll([]Node{{ID: 1, Level: 6}})
	list := b.List()
	if len(list) != 1 || list[0].ID != 1 || list[0].Level != 6 {
		t.Fatalf("snapshot must REPLACE, got %+v", list)
	}
}

func TestApplyUpsertPreservesLevelWhenAbsent(t *testing.T) {
	b := New()
	b.ReplaceAll([]Node{{ID: 1, Level: 5, Progress: 0.2}})
	// Delta with a new progress but NO level (hasLevel=false) keeps the level.
	b.Apply(Node{ID: 1, Progress: 0.6}, false, true, false)
	if got := b.List()[0]; got.Level != 5 || got.Progress != 0.6 {
		t.Fatalf("level must survive a level-less delta: %+v", got)
	}
	// A level-only delta (no progress, no fame) must NOT zero accumulated fame/progress.
	b.ReplaceAll([]Node{{ID: 1, Level: 5, Progress: 0.6, Fame: 999}})
	b.Apply(Node{ID: 1, Level: 6}, true, false, false)
	if got := b.List()[0]; got.Level != 6 || got.Fame != 999 || got.Progress != 0.6 {
		t.Fatalf("level-only delta must preserve fame/progress: %+v", got)
	}
	// Delta with a level updates it.
	b.Apply(Node{ID: 1, Level: 6, Progress: 0.1}, true, true, false)
	if got := b.List()[0]; got.Level != 6 {
		t.Fatalf("level delta ignored: %+v", got)
	}
	// A delta for an unknown node creates a row (a later snapshot reconciles).
	b.Apply(Node{ID: 77, Level: 1, Progress: 0.3}, true, true, false)
	if _, ok := b.get(77); !ok {
		t.Fatal("delta for unknown node must create a row")
	}
}

func TestCompleteResetsProgress(t *testing.T) {
	b := New()
	b.ReplaceAll([]Node{{ID: 1, Level: 5, Progress: 0.9}})
	b.Complete(1, 6)
	if got := b.List()[0]; got.Level != 6 || got.Progress != 0 {
		t.Fatalf("complete must set level + zero progress: %+v", got)
	}
}

func TestListOrdering(t *testing.T) {
	b := New()
	b.ReplaceAll([]Node{
		{ID: 1, Level: 3, Fame: 10},
		{ID: 2, Level: 5, Fame: 5},
		{ID: 3, Level: 5, Fame: 50},
	})
	got := b.List()
	// level-desc, then fame-desc: 3 (L5,F50), 2 (L5,F5), 1 (L3).
	if got[0].ID != 3 || got[1].ID != 2 || got[2].ID != 1 {
		t.Fatalf("ordering wrong: %+v", got)
	}
}

func TestBoundedCap(t *testing.T) {
	b := New()
	big := make([]Node, maxNodes+50)
	for i := range big {
		big[i] = Node{ID: i, Level: 1}
	}
	b.ReplaceAll(big)
	if n, _ := b.Totals(); n > maxNodes {
		t.Fatalf("cap breached: %d", n)
	}
}

// XI: repeated snapshots never accrete; cap holds under a flood.
func TestSoakBounded(t *testing.T) {
	b := New()
	for r := 0; r < 50; r++ {
		nodes := make([]Node, 700)
		for i := range nodes {
			nodes[i] = Node{ID: i, Level: r % 100, Fame: int64(i)}
		}
		b.ReplaceAll(nodes)
		for i := 0; i < 200; i++ {
			b.Apply(Node{ID: i, Level: 1, Progress: 0.5}, true, true, false)
		}
	}
	if n, _ := b.Totals(); n != 700 {
		t.Fatalf("accretion: %d nodes after 50 snapshots", n)
	}
}
