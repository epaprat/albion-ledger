// Package specboard holds the player's live Destiny Board state (feature 011): a
// full snapshot arrives every zone join (E:154), live progress deltas (E:153) and
// node completions (E:152) between snapshots. Pure domain — no wire or UI types.
// Touched only on the single capture goroutine (no internal locking).
package specboard

import "sort"

// maxNodes bounds the board (Principle XI). The real Destiny Board is ~700 nodes;
// anything past this is malformed/hostile, not a real snapshot.
const maxNodes = 2048

// Node is one Destiny Board entry.
type Node struct {
	ID       int
	Level    int
	Progress float64 // fraction to the next level, [0,1]
	Fame     int64   // total accumulated fame on this node
}

// Board is the current node set.
type Board struct {
	nodes map[int]Node
}

// New returns an empty board.
func New() *Board { return &Board{nodes: map[int]Node{}} }

// ReplaceAll installs a full snapshot as the authority (E:154): the previous state
// is discarded so a delta can never outlive the snapshot that excludes it. Excess
// nodes past the cap are dropped.
func (b *Board) ReplaceAll(nodes []Node) {
	b.nodes = make(map[int]Node, len(nodes))
	for _, n := range nodes {
		if len(b.nodes) >= maxNodes {
			break
		}
		b.nodes[n.ID] = n
	}
}

// MergeAll upserts a batch WITHOUT clearing — the Destiny Board snapshot arrives as
// SEVERAL E:154 packets per join (one per ring/category, live-seen 75+75+36), so all
// but the first in a burst merge into the same board (011).
func (b *Board) MergeAll(nodes []Node) {
	for _, n := range nodes {
		if _, ok := b.nodes[n.ID]; !ok && len(b.nodes) >= maxNodes {
			continue
		}
		b.nodes[n.ID] = n
	}
}

// Apply upserts one node from a delta (E:153). When the delta carries no level
// (hasLevel=false) the existing level is preserved; an unknown node is created (a
// later snapshot reconciles). Progress/Fame always take the delta's values.
func (b *Board) Apply(n Node, hasLevel bool) {
	cur, ok := b.nodes[n.ID]
	if ok && !hasLevel {
		n.Level = cur.Level
	}
	if !ok && len(b.nodes) >= maxNodes {
		return
	}
	b.nodes[n.ID] = n
}

// Complete records a node level-up (E:152): set the new level, reset progress.
func (b *Board) Complete(id, level int) {
	n := b.nodes[id]
	n.ID, n.Level, n.Progress = id, level, 0
	if _, ok := b.nodes[id]; !ok && len(b.nodes) >= maxNodes {
		return
	}
	b.nodes[id] = n
}

// List returns every node sorted level-desc then fame-desc (name resolution and
// display happen in the adapter layer).
func (b *Board) List() []Node {
	out := make([]Node, 0, len(b.nodes))
	for _, n := range b.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level > out[j].Level
		}
		if out[i].Fame != out[j].Fame {
			return out[i].Fame > out[j].Fame
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Totals returns the node count and summed fame.
func (b *Board) Totals() (nodeCount int, totalFame int64) {
	for _, n := range b.nodes {
		totalFame += n.Fame
	}
	return len(b.nodes), totalFame
}

func (b *Board) get(id int) (Node, bool) { n, ok := b.nodes[id]; return n, ok }
