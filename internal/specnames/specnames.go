// Package specnames resolves Destiny Board node ids to readable names + categories
// (feature 011). The catalog is data-driven: bundled at build time, overridable at
// runtime via -specnodes (FR-012 pattern), and a malformed file keeps the previous
// catalog rather than blanking every node.
package specnames

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type entry struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

type fileFormat struct {
	Nodes []entry `json:"nodes"`
}

// Catalog maps node id → (name, category).
type Catalog struct {
	mu   sync.RWMutex
	byID map[int]entry
}

// New builds a catalog from JSON bytes (e.g. the embedded default).
func New(jsonBytes []byte) (*Catalog, error) {
	m, err := parse(jsonBytes)
	if err != nil {
		return nil, err
	}
	return &Catalog{byID: m}, nil
}

func parse(b []byte) (map[int]entry, error) {
	var f fileFormat
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("specnames: %w", err)
	}
	m := make(map[int]entry, len(f.Nodes))
	for _, n := range f.Nodes {
		m[n.ID] = n
	}
	return m, nil
}

// Resolve returns the node's name + category; ok=false for an unknown id, which the
// caller renders as an honest "Node #N" placeholder (spec rule 5).
func (c *Catalog) Resolve(id int) (name, category string, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, found := c.byID[id]
	return e.Name, e.Category, found
}

// Reload swaps in a new catalog file at runtime; a malformed file is rejected and
// the previous catalog is kept (FR-012, Principle IV).
func (c *Catalog) Reload(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m, err := parse(b)
	if err != nil {
		return err // keep previous
	}
	c.mu.Lock()
	c.byID = m
	c.mu.Unlock()
	return nil
}
