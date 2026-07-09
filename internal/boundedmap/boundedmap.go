// Package boundedmap provides a bounded key→value cache with FIFO (oldest-inserted)
// eviction. It replaces the ad-hoc "map + insertion-order slice + evict-at-cap" pattern
// that was hand-rolled across the codebase (offerCache, mailInfo, objReg …), giving the
// bounded-memory guarantee (Constitution Principle XI) one tested home.
//
// It intentionally holds NO lock of its own: callers that already serialize access with
// their own mutex keep it, so wrapping this in a struct field never introduces a second
// lock (or a double-lock). This mirrors the original call sites exactly.
//
// This is NOT pending.Map: there is no Take (remove-on-read) and no TTL. Entries live
// until evicted by capacity pressure — the semantics of a keep-warm FIFO cache.
package boundedmap

// Map is a bounded FIFO cache. The zero value is not usable; construct with New.
type Map[K comparable, V any] struct {
	m     map[K]V
	order []K // insertion order, oldest at index 0
	cap   int
}

// New returns a Map bounded to capacity entries (clamped to a minimum of 1).
func New[K comparable, V any](capacity int) *Map[K, V] {
	if capacity < 1 {
		capacity = 1
	}
	return &Map[K, V]{m: make(map[K]V, capacity), cap: capacity}
}

// Put stores v under k. A NEW key inserted at capacity first evicts the oldest-inserted
// key; an EXISTING key updates its value WITHOUT changing its eviction position (the
// behaviour the replaced call sites relied on — a re-seen order id refreshes its data but
// does not jump the eviction queue). Len never exceeds the capacity.
func (b *Map[K, V]) Put(k K, v V) {
	if _, exists := b.m[k]; !exists {
		if len(b.m) >= b.cap && len(b.order) > 0 {
			delete(b.m, b.order[0])
			b.order = b.order[1:]
		}
		b.order = append(b.order, k)
	}
	b.m[k] = v
}

// Get returns the value stored under k and whether it was present.
func (b *Map[K, V]) Get(k K) (V, bool) {
	v, ok := b.m[k]
	return v, ok
}

// Len is the number of live entries (always ≤ capacity).
func (b *Map[K, V]) Len() int { return len(b.m) }
