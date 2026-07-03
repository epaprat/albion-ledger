package loot

import (
	"fmt"
	"testing"
)

// Contract rule 1: full chain → exactly one Hit; repeat of the same move → none.
func TestFullChainSingleHit(t *testing.T) {
	tr := New()
	tr.RegisterSource(500, "Elder Treant", 1000)
	if hits := tr.AttachContainer("c1", 500, []int{0, 0, 777}, 1001); len(hits) != 0 {
		t.Fatalf("attach alone must not hit: %v", hits)
	}
	hits := tr.ResolveMove("c1", 2, 1002)
	if len(hits) != 1 || hits[0].ItemObjID != 777 || hits[0].Source != "Elder Treant" {
		t.Fatalf("hits = %+v, want one {777, Elder Treant}", hits)
	}
	if again := tr.ResolveMove("c1", 2, 1003); len(again) != 0 {
		t.Fatalf("repeat move must dedup: %v", again)
	}
}

// Contract rule 2: move BEFORE attach → pending, resolved when the container arrives.
func TestMoveBeforeAttachResolvesViaPending(t *testing.T) {
	tr := New()
	tr.RegisterSource(500, "Chest", 1000)
	if hits := tr.ResolveMove("c1", 1, 1001); len(hits) != 0 {
		t.Fatalf("unknown container must queue, not hit: %v", hits)
	}
	hits := tr.AttachContainer("c1", 500, []int{0, 888}, 1002)
	if len(hits) != 1 || hits[0].ItemObjID != 888 {
		t.Fatalf("attach must resolve the pending move: %+v", hits)
	}
	if p, _, _ := tr.Stats(); p != 0 {
		t.Fatalf("pending should be drained, got %d", p)
	}
}

// Contract rule 3: a KNOWN container without a loot source (bank) never hits and
// never queues.
func TestNonLootContainerNeverHits(t *testing.T) {
	tr := New()
	tr.AttachContainer("bank", 42, []int{111, 222}, 1000) // srcObjID 42 never registered
	if hits := tr.ResolveMove("bank", 0, 1001); len(hits) != 0 {
		t.Fatalf("bank move must not hit: %v", hits)
	}
	if p, _, _ := tr.Stats(); p != 0 {
		t.Fatalf("bank move must not queue, pending = %d", p)
	}
}

// Contract rule 4: "take all" (given items) → one Hit per item.
func TestMoveGivenItemsAllCounted(t *testing.T) {
	tr := New()
	tr.RegisterSource(500, "Corpse", 1000)
	tr.AttachContainer("c1", 500, []int{10, 20, 30}, 1001)
	hits := tr.ResolveMoveGiven("c1", []int{10, 20, 30}, 1002)
	if len(hits) != 3 {
		t.Fatalf("take-all → %d hits, want 3", len(hits))
	}
	// Double sighting of the same pickup → nothing new (rule 5).
	if again := tr.ResolveMoveGiven("c1", []int{10, 20, 30}, 1003); len(again) != 0 {
		t.Fatalf("double sighting must dedup: %v", again)
	}
}

// Contract rule 6: pending TTL expiry is counted, never silent.
func TestPendingExpiryCounted(t *testing.T) {
	tr := New()
	tr.ResolveMove("ghost", 0, 1000) // container never arrives
	// Any later call past the TTL sweeps it out.
	tr.RegisterSource(1, "x", 1000+pendingTTLMS+1)
	p, expired, _ := tr.Stats()
	if p != 0 || expired != 1 {
		t.Fatalf("pending=%d expired=%d, want 0/1", p, expired)
	}
}

// Contract rule 7: caps enforced (spot: pending + sources).
func TestCapsEnforced(t *testing.T) {
	tr := New()
	for i := 0; i < pendingCap+10; i++ {
		tr.ResolveMove(fmt.Sprintf("g%d", i), 0, 1000)
	}
	p, _, droppedCap := tr.Stats()
	if p > pendingCap {
		t.Fatalf("pending %d exceeds cap %d", p, pendingCap)
	}
	if droppedCap != 10 {
		t.Fatalf("droppedCap = %d, want 10", droppedCap)
	}
	for i := 0; i < sourceCap+5; i++ {
		tr.RegisterSource(1000+i, "s", 2000)
	}
	tr.mu.Lock()
	n := len(tr.sources)
	tr.mu.Unlock()
	if n > sourceCap {
		t.Fatalf("sources %d exceed cap %d", n, sourceCap)
	}
}

// Contract rule 8: empty slots preserved — slot index aligns with the move request.
func TestEmptySlotsPreserveAlignment(t *testing.T) {
	tr := New()
	tr.RegisterSource(500, "Chest", 1000)
	tr.AttachContainer("c1", 500, []int{0, 0, 0, 999}, 1001)
	if hits := tr.ResolveMove("c1", 0, 1002); len(hits) != 0 {
		t.Fatalf("empty slot must not hit: %v", hits)
	}
	hits := tr.ResolveMove("c1", 3, 1003)
	if len(hits) != 1 || hits[0].ItemObjID != 999 {
		t.Fatalf("slot 3 must resolve 999: %+v", hits)
	}
	// Out-of-range slot: tolerated, no hit, no panic (Principle IV).
	if hits := tr.ResolveMove("c1", 9, 1004); len(hits) != 0 {
		t.Fatalf("out-of-range slot must not hit: %v", hits)
	}
}

// Re-attach REPLACES the slot map (loot container after partial pickup).
func TestReattachReplacesSlots(t *testing.T) {
	tr := New()
	tr.RegisterSource(500, "Corpse", 1000)
	tr.AttachContainer("c1", 500, []int{111, 222}, 1001)
	tr.ResolveMove("c1", 0, 1002) // takes 111
	tr.AttachContainer("c1", 500, []int{0, 222}, 1003)
	hits := tr.ResolveMove("c1", 1, 1004)
	if len(hits) != 1 || hits[0].ItemObjID != 222 {
		t.Fatalf("post-reattach slot 1 → %+v, want 222", hits)
	}
}

// Dedup window: a reused object id AFTER the window is a different item (per-zone id
// reuse) and must count again; inside the window it is a re-sighting and must not.
func TestDedupWindowAllowsIdReuse(t *testing.T) {
	tr := New()
	tr.RegisterSource(500, "A", 1000)
	tr.AttachContainer("c1", 500, []int{777}, 1001)
	if hits := tr.ResolveMove("c1", 0, 1002); len(hits) != 1 {
		t.Fatalf("first pickup must hit: %v", hits)
	}
	if hits := tr.ResolveMove("c1", 0, 1002+dedupWindowMS); len(hits) != 0 {
		t.Fatalf("re-sighting inside window must dedup: %v", hits)
	}
	// Later zone: same objID on a fresh loot container = different item → counts.
	later := int64(1002 + dedupWindowMS + 1)
	tr.RegisterSource(900, "B", later)
	tr.AttachContainer("c2", 900, []int{777}, later)
	if hits := tr.ResolveMove("c2", 0, later+1); len(hits) != 1 {
		t.Fatalf("reused objID past the window must count again: %v", hits)
	}
}

// Resolve-time TTL: an expired source must not match even if the FIFO sweep hasn't
// evicted it yet (a refreshed head can block the sweep indefinitely).
func TestExpiredSourceDoesNotMatchAtResolve(t *testing.T) {
	tr := New()
	tr.RegisterSource(500, "Old", 1000)
	// Keep a refreshed head in front so the sweep can't reach entry 500.
	tr.RegisterSource(1, "Head", 1000)
	tr.RegisterSource(1, "Head", 1000+sourceTTLMS) // refresh in place
	tr.AttachContainer("c1", 500, []int{777}, 1000+sourceTTLMS+1)
	if hits := tr.ResolveMove("c1", 0, 1000+sourceTTLMS+2); len(hits) != 0 {
		t.Fatalf("expired source must not produce hits: %v", hits)
	}
}

// Bank-sized containers never loot-resolve, even if their source id collides with a
// registered lootable (small per-zone ids make that collision possible).
func TestBankSizedContainerNeverLoots(t *testing.T) {
	tr := New()
	tr.RegisterSource(6, "collision", 1000) // a lootable announced with a tiny id
	slots := make([]int, 128)               // bank tab
	for i := range slots {
		slots[i] = 10_000 + i
	}
	tr.AttachContainer("bank", 6, slots, 1001)
	if hits := tr.ResolveMove("bank", 3, 1002); len(hits) != 0 {
		t.Fatalf("bank-sized container must never loot: %v", hits)
	}
}

// Bounded-state soak (T015): heavy mixed load keeps every structure within caps.
func TestSoakBoundedState(t *testing.T) {
	tr := New()
	ts := int64(1000)
	for i := 0; i < 100_000; i++ {
		ts += 50
		switch i % 4 {
		case 0:
			tr.RegisterSource(i, "s", ts)
		case 1:
			tr.AttachContainer(fmt.Sprintf("c%d", i%1000), i-1, []int{i + 1}, ts)
		case 2:
			tr.ResolveMove(fmt.Sprintf("c%d", i%1000), 0, ts)
		case 3:
			tr.ResolveMoveGiven(fmt.Sprintf("g%d", i%2000), []int{i}, ts)
		}
	}
	tr.mu.Lock()
	ns, nc, np, nr := len(tr.sources), len(tr.containers), len(tr.pending), len(tr.recorded)
	tr.mu.Unlock()
	if ns > sourceCap || nc > containerCap || np > pendingCap || nr > dedupCap {
		t.Fatalf("state exceeded caps: sources=%d containers=%d pending=%d recorded=%d", ns, nc, np, nr)
	}
}
