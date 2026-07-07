// Package pending is the ONE bounded pending-match structure behind the capture
// pipeline's "X arrived before its declaration" queues. Three consumers (loot hits,
// holdings puts, own-state inventory slots) previously carried three hand-rolled
// copies of the same cap+TTL+counter idiom, and the copies drifted once in the wild
// (007: one gained a TTL months before the other) — Principle XI guarantees now come
// from a single implementation (ADR-022/024/025, feature 009).
//
// Not goroutine-safe by design: every consumer runs on the single capture goroutine
// and already serializes access with the object-registry mutex. Timestamps are passed
// in (no clock dependency) so tests stay deterministic.
package pending

type entry[V any] struct {
	val    V
	seenMS int64
}

// Map holds at most cap entries for at most ttlMS milliseconds (0 = forever).
// Losses — cap overflow, sweep expiry, stale Take — are counted, never silent.
type Map[V any] struct {
	entries map[int]entry[V]
	cap     int
	ttlMS   int64
	dropped int
}

// New returns an empty Map with the given bounds. ttlMS 0 disables expiry.
func New[V any](capacity int, ttlMS int64) *Map[V] {
	return &Map[V]{entries: map[int]entry[V]{}, cap: capacity, ttlMS: ttlMS}
}

// Queue inserts (or overwrites) an entry, sweeping expired ones first — the maps are
// tiny (≤ cap), so the inline sweep is cheaper than bookkeeping a separate timer.
// A FULL map drops the write (counted) even for an already-present id: the pre-009
// copies behaved exactly this way, and refreshing an existing entry's TTL at cap
// would let a hostile re-queue stream keep entries alive forever (009 review).
func (m *Map[V]) Queue(id int, v V, nowMS int64) {
	if m.ttlMS > 0 {
		for k, e := range m.entries {
			if nowMS-e.seenMS > m.ttlMS {
				delete(m.entries, k)
				m.dropped++
			}
		}
	}
	if len(m.entries) >= m.cap { // full: refuse ALL writes (even re-queues), counted
		m.dropped++
		return
	}
	m.entries[id] = entry[V]{val: v, seenMS: nowMS}
}

// Take removes and returns the entry for id. A past-TTL entry is a miss WITHOUT a
// counted drop: expiry losses are counted once, on the Queue-path sweep — the pre-009
// drain path never incremented the counter, and double-counting would drift the
// rate-limited loss logs (FR-004 observability preserved bit-for-bit, 009 review).
func (m *Map[V]) Take(id int, nowMS int64) (V, bool) {
	e, ok := m.entries[id]
	if !ok {
		var zero V
		return zero, false
	}
	delete(m.entries, id)
	if m.ttlMS > 0 && nowMS-e.seenMS > m.ttlMS {
		var zero V
		return zero, false
	}
	return e.val, true
}

// Clear removes entries whose value matches pred — snapshot hygiene (an authoritative
// snapshot supersedes queued updates), so removals are NOT counted as losses.
func (m *Map[V]) Clear(pred func(V) bool) {
	for k, e := range m.entries {
		if pred(e.val) {
			delete(m.entries, k)
		}
	}
}

// Len reports the current entry count.
func (m *Map[V]) Len() int { return len(m.entries) }

// Dropped reports total counted losses (cap overflow + expiry) since creation.
func (m *Map[V]) Dropped() int { return m.dropped }
