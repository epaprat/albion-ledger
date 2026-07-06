// Package specenum holds the E:1 board enumeration (feature 012): the position→node-id
// order that a warm-login E:1 uses when it omits the id list (k2). A cold login/relog
// sends k2 (the ids) explicitly and is self-describing; we persist that order so a
// later k2-less E:1 (whose k3 is position-indexed against the same order) still decodes.
// Pure — touched only on the capture goroutine.
package specenum

// Enum maps an E:1 k3 position to its node id.
type Enum struct {
	pos2id []int
}

// New returns an empty enumeration.
func New() *Enum { return &Enum{} }

// SetFromWire installs the authoritative id order carried by a cold-login E:1 (k2).
func (e *Enum) SetFromWire(ids []int) {
	e.pos2id = append(e.pos2id[:0], ids...)
}

// Known reports whether an enumeration has been learned yet.
func (e *Enum) Known() bool { return len(e.pos2id) > 0 }

// Snapshot returns a copy of the id order for persistence.
func (e *Enum) Snapshot() []int { return append([]int(nil), e.pos2id...) }

// Restore loads a persisted id order.
func (e *Enum) Restore(ids []int) { e.pos2id = append(e.pos2id[:0], ids...) }

// Pair is one decoded board node.
type Pair struct {
	ID    int
	Level int
}

// Decode turns a board's (ids, levels) into id→level pairs. When ids is present (cold
// login k2) it is used directly and also learned as the enumeration. When ids is nil
// (warm login) the levels are mapped through the learned enumeration — but ONLY when the
// learned order is exactly as long as the level array. Any length mismatch means the
// persisted order is for a different board version (an Albion patch added/removed/
// reordered nodes between sessions): decoding through it would attribute levels to the
// wrong node ids and show wrong maxed nodes (FR-004). On mismatch — or with no learned
// order — Decode returns nil and the caller falls back to E:154/E:155 (feature 011).
func (e *Enum) Decode(ids, levels []int) []Pair {
	if len(ids) == len(levels) && len(ids) > 0 {
		e.SetFromWire(ids)
		out := make([]Pair, len(levels))
		for i := range levels {
			out[i] = Pair{ID: ids[i], Level: levels[i]}
		}
		return out
	}
	// Warm login: the enum must match the wire k3 length EXACTLY — a stale/partial order
	// is refused rather than silently truncated (which would mislabel or drop maxed nodes).
	if len(e.pos2id) != len(levels) || len(levels) == 0 {
		return nil
	}
	out := make([]Pair, len(levels))
	for i := range levels {
		out[i] = Pair{ID: e.pos2id[i], Level: levels[i]}
	}
	return out
}
