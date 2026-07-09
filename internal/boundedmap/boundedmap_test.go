package boundedmap

import "testing"

// Rule 1 — a new key at capacity evicts the OLDEST inserted key; Len never exceeds cap.
func TestEvictsOldestAtCap(t *testing.T) {
	b := New[int, string](3)
	b.Put(1, "a")
	b.Put(2, "b")
	b.Put(3, "c")
	b.Put(4, "d") // evicts 1 (oldest)
	if b.Len() != 3 {
		t.Fatalf("Len must stay at cap 3, got %d", b.Len())
	}
	if _, ok := b.Get(1); ok {
		t.Fatal("oldest key 1 must be evicted")
	}
	for _, k := range []int{2, 3, 4} {
		if _, ok := b.Get(k); !ok {
			t.Fatalf("key %d must survive", k)
		}
	}
}

// Rule 2 — re-Put of an existing key refreshes its value but does NOT change its eviction
// position (it does not jump the queue), matching the offerCache/mailInfo/objReg behaviour.
func TestReinsertRefreshesValueKeepsPosition(t *testing.T) {
	b := New[int, string](3)
	b.Put(1, "a")
	b.Put(2, "b")
	b.Put(3, "c")
	b.Put(1, "A") // refresh value of the OLDEST key — must not move it to the back
	if v, _ := b.Get(1); v != "A" {
		t.Fatalf("value must refresh to A, got %q", v)
	}
	if b.Len() != 3 {
		t.Fatalf("reinsert must not grow beyond cap, got %d", b.Len())
	}
	b.Put(4, "d") // still evicts 1 (its position was unchanged by the refresh)
	if _, ok := b.Get(1); ok {
		t.Fatal("refreshed-but-oldest key 1 must still be the one evicted")
	}
}

// Rule 3 — Get on a missing key returns the zero value and false.
func TestGetMiss(t *testing.T) {
	b := New[string, int](2)
	if v, ok := b.Get("nope"); ok || v != 0 {
		t.Fatalf("miss must be (0,false), got (%d,%v)", v, ok)
	}
}

// Rule 4 — the invariant Len == len(order) ≤ cap holds across a long churn.
func TestInvariantUnderChurn(t *testing.T) {
	b := New[int, int](8)
	for i := 0; i < 1000; i++ {
		b.Put(i, i*i)
		if b.Len() > 8 {
			t.Fatalf("Len exceeded cap at i=%d: %d", i, b.Len())
		}
		if len(b.order) != b.Len() {
			t.Fatalf("order/len drift at i=%d: order=%d len=%d", i, len(b.order), b.Len())
		}
	}
	// Only the last 8 keys remain.
	for i := 992; i < 1000; i++ {
		if v, ok := b.Get(i); !ok || v != i*i {
			t.Fatalf("recent key %d lost", i)
		}
	}
}

// Rule 5 — generic over key types actually used by the call sites (int64/int/string).
func TestGenericKeyTypes(t *testing.T) {
	bi := New[int64, string](2)
	bi.Put(int64(1), "x")
	if v, ok := bi.Get(int64(1)); !ok || v != "x" {
		t.Fatal("int64 key failed")
	}
	bs := New[string, struct{}](2) // bounded-set shape (flow dedup candidate)
	bs.Put("k", struct{}{})
	if _, ok := bs.Get("k"); !ok {
		t.Fatal("string set key failed")
	}
	// cap clamps below 1 to 1.
	if New[int, int](0).cap != 1 {
		t.Fatal("capacity < 1 must clamp to 1")
	}
}
