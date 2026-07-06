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
// (warm login) the levels are mapped through the learned enumeration; if none is
// known yet, Decode returns nil (the caller falls back to E:154/E:155 — feature 011).
func (e *Enum) Decode(ids, levels []int) []Pair {
	if len(ids) == len(levels) && len(ids) > 0 {
		e.SetFromWire(ids)
		out := make([]Pair, len(levels))
		for i := range levels {
			out[i] = Pair{ID: ids[i], Level: levels[i]}
		}
		return out
	}
	if !e.Known() {
		return nil
	}
	n := len(levels)
	if len(e.pos2id) < n {
		n = len(e.pos2id) // decode only the positions we know
	}
	out := make([]Pair, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Pair{ID: e.pos2id[i], Level: levels[i]})
	}
	return out
}
