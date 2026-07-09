package wailsadapter

import "testing"

// 019 US4 / SC-003 — BeginFlowBatch/EndFlowBatch coalesce a burst of loot ingests (a
// take-all move) into ONE flow-changed refresh, while every event still lands in the
// ledger (content unchanged, only the emit count drops from N to 1).
func TestFlowBatch_CoalescesEmitsKeepsEvents(t *testing.T) {
	s, em, book := newHoldSvc(t)
	book.SetEMV(920, 0, 100, 1000) // value the item so each loot ingest is a real event

	// Unbatched baseline: 3 ingests → 3 refreshes.
	s.IngestLoot("lt:1", 920, 0, 1, 1000, "corpse")
	s.IngestLoot("lt:2", 920, 0, 1, 1000, "corpse")
	s.IngestLoot("lt:3", 920, 0, 1, 1000, "corpse")
	if n := countEvent(em, EventFlowChanged); n != 3 {
		t.Fatalf("unbatched: want 3 flow emits, got %d", n)
	}

	// Batched: 3 more ingests → exactly 1 coalesced refresh.
	em.events = nil
	s.BeginFlowBatch()
	s.IngestLoot("lt:4", 920, 0, 1, 1000, "corpse")
	s.IngestLoot("lt:5", 920, 0, 1, 1000, "corpse")
	s.IngestLoot("lt:6", 920, 0, 1, 1000, "corpse")
	s.EndFlowBatch()
	if n := countEvent(em, EventFlowChanged); n != 1 {
		t.Fatalf("batched: want 1 coalesced flow emit, got %d", n)
	}

	// All six events are in the ledger — batching changed the refresh count, not the data.
	if ec := s.FlowSummary().EventCount; ec != 6 {
		t.Fatalf("all 6 loot events must land regardless of batching, got %d", ec)
	}
}

// An empty batch (no ingests between Begin/End) emits nothing.
func TestFlowBatch_EmptyEmitsNothing(t *testing.T) {
	s, em, _ := newHoldSvc(t)
	s.BeginFlowBatch()
	s.EndFlowBatch()
	if n := countEvent(em, EventFlowChanged); n != 0 {
		t.Fatalf("empty batch must emit nothing, got %d", n)
	}
}
