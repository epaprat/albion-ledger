// Package codes is the data-driven message code → category registry. Game patches
// shift codes, so this map is external data, hot-swappable at runtime (FR-012):
// a shifted code is a JSON edit + reload, never a recompile.
package codes

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

type fileFormat struct {
	Entries []model.CodeMapEntry `json:"entries"`
}

type entry struct {
	category model.Category
	guardKey int
	hasGuard bool
}

// Registry maps (kind, code) → category (+ optional guard key).
type Registry struct {
	mu sync.RWMutex
	m  map[string]entry
}

// New builds a registry from JSON bytes (e.g. the embedded default).
func New(jsonBytes []byte) (*Registry, error) {
	m, err := parse(jsonBytes)
	if err != nil {
		return nil, err
	}
	return &Registry{m: m}, nil
}

func parse(jsonBytes []byte) (map[string]entry, error) {
	var f fileFormat
	if err := json.Unmarshal(jsonBytes, &f); err != nil {
		return nil, fmt.Errorf("codes: %w", err)
	}
	m := make(map[string]entry, len(f.Entries))
	for _, e := range f.Entries {
		ent := entry{category: e.Category}
		if e.GuardKey != nil {
			ent.guardKey = *e.GuardKey
			ent.hasGuard = true
		}
		m[key(e.Kind, e.Code)] = ent
	}
	return m, nil
}

// Lookup returns the category and optional guard key for a (kind, code). An
// unlisted code is unhandled (ok=false) — this is how position/radar codes stay
// excluded (ToS-safe).
func (r *Registry) Lookup(kind string, code int) (model.Category, int, bool, bool) {
	r.mu.RLock()
	e, ok := r.m[key(kind, code)]
	r.mu.RUnlock()
	if !ok {
		return "", 0, false, false
	}
	return e.category, e.guardKey, e.hasGuard, true
}

// Reload swaps in a new code map; a malformed file is rejected and the previous
// map is kept (FR-012, Principle IV).
func (r *Registry) Reload(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m, err := parse(b)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.m = m
	r.mu.Unlock()
	return nil
}

func key(kind string, code int) string { return kind + ":" + strconv.Itoa(code) }
