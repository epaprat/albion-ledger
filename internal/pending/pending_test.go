package pending

import "testing"

// Queue + Take round-trip; Take removes the entry.
func TestQueueTake(t *testing.T) {
	m := New[string](4, 10_000)
	m.Queue(1, "corpse", 1000)
	v, ok := m.Take(1, 2000)
	if !ok || v != "corpse" {
		t.Fatalf("Take = %q/%v, want corpse/true", v, ok)
	}
	if _, ok := m.Take(1, 2000); ok {
		t.Fatal("second Take must miss (entry consumed)")
	}
}

// Take past the TTL misses WITHOUT counting — expiry is counted once, on the
// Queue-path sweep (pre-009 drain semantics, pinned by the 009 review: a counted
// stale Take would drift the rate-limited loss logs).
func TestTakeTTLGuard(t *testing.T) {
	m := New[string](4, 10_000)
	m.Queue(1, "corpse", 1000)
	if _, ok := m.Take(1, 11_001); ok {
		t.Fatal("Take past TTL must miss")
	}
	if m.Dropped() != 0 {
		t.Fatalf("stale take must NOT count (queue sweep owns expiry): dropped=%d", m.Dropped())
	}
	if m.Len() != 0 {
		t.Fatal("stale entry must still be removed")
	}
}

// Queue sweeps expired entries inline (counted), exactly like the 007/008 pattern.
func TestQueueInlineSweep(t *testing.T) {
	m := New[string](4, 10_000)
	m.Queue(1, "a", 1000)
	m.Queue(2, "b", 2000)
	m.Queue(3, "c", 13_000) // 1 (age 12s) expired and swept; 2 (11s) too
	if m.Len() != 1 {
		t.Fatalf("len = %d, want 1 (two swept)", m.Len())
	}
	if m.Dropped() != 2 {
		t.Fatalf("dropped = %d, want 2", m.Dropped())
	}
}

// At cap the new entry is dropped and counted (no eviction — matches current code).
func TestCapDropsCounted(t *testing.T) {
	m := New[string](2, 10_000)
	m.Queue(1, "a", 1000)
	m.Queue(2, "b", 1000)
	m.Queue(3, "c", 1000)
	if m.Len() != 2 {
		t.Fatalf("len = %d, want cap 2", m.Len())
	}
	if _, ok := m.Take(3, 1500); ok {
		t.Fatal("over-cap entry must not exist")
	}
	if m.Dropped() != 1 {
		t.Fatalf("dropped = %d, want 1", m.Dropped())
	}
}

// TTL 0 = entries never expire (pendingInv behavior).
func TestZeroTTLNeverExpires(t *testing.T) {
	m := New[string](4, 0)
	m.Queue(1, "bag-guid", 1000)
	m.Queue(2, "x", 9_999_999) // sweep must not touch id 1
	if v, ok := m.Take(1, 99_999_999); !ok || v != "bag-guid" {
		t.Fatalf("TTL-0 entry must survive forever, got %q/%v", v, ok)
	}
	if m.Dropped() != 0 {
		t.Fatalf("dropped = %d, want 0", m.Dropped())
	}
}

// Clear removes entries matching the predicate (snapshot hygiene), silently.
func TestClearPredicate(t *testing.T) {
	m := New[string](8, 0)
	m.Queue(1, "self-bag", 1000)
	m.Queue(2, "self-equipped", 1000)
	m.Queue(3, "bank", 1000)
	m.Clear(func(v string) bool { return v == "self-bag" || v == "self-equipped" })
	if m.Len() != 1 {
		t.Fatalf("len = %d, want 1", m.Len())
	}
	if _, ok := m.Take(3, 2000); !ok {
		t.Fatal("non-matching entry must survive Clear")
	}
	if m.Dropped() != 0 {
		t.Fatal("Clear is hygiene, not loss — no drop counting")
	}
}

// Re-queueing an existing id below cap overwrites value and timestamp; AT cap even a
// re-queue is refused and counted (pre-009 semantics — a full map accepts nothing,
// so a hostile re-queue stream cannot keep extending an entry's TTL).
func TestRequeueOverwrites(t *testing.T) {
	m := New[string](4, 10_000)
	m.Queue(1, "old", 1000)
	m.Queue(1, "new", 9000)
	if v, ok := m.Take(1, 12_000); !ok || v != "new" {
		t.Fatalf("re-queue must overwrite: %q/%v", v, ok)
	}

	full := New[string](2, 10_000)
	full.Queue(1, "a", 1000)
	full.Queue(2, "b", 1000)
	full.Queue(1, "a2", 2000) // at cap: refused, counted
	if v, _ := full.Take(1, 3000); v != "a" {
		t.Fatalf("at-cap re-queue must not overwrite: got %q", v)
	}
	if full.Dropped() != 1 {
		t.Fatalf("at-cap re-queue must count: dropped=%d", full.Dropped())
	}
}
