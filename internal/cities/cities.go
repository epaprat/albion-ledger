// Package cities maps Albion location/cluster ids to human city names for the
// holdings-by-location view (FR-003/FR-005). An unmapped id is rendered as an
// explicit "Unknown city (<id>)" — never guessed as a wrong city.
package cities

import (
	"encoding/json"
	"strings"
)

// Table resolves a location id to a display name.
type Table struct {
	byID map[string]string
}

type fileShape struct {
	Cities map[string]string `json:"cities"`
}

// New parses a cities table from JSON ({"cities":{"<id>":"<name>"}}). A malformed
// file yields an empty (but usable) table; every id then falls back gracefully.
func New(data []byte) *Table {
	t := &Table{byID: map[string]string{}}
	var f fileShape
	if err := json.Unmarshal(data, &f); err == nil {
		for id, name := range f.Cities {
			t.byID[id] = name
		}
	}
	return t
}

// Name returns the display name for a location id. "@ISLAND@…" → "Island"; a known
// id → its mapped name; anything else → "Unknown city (<id>)". An empty id → "".
func (t *Table) Name(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToUpper(id), "@ISLAND@") {
		return "Island"
	}
	if t != nil {
		if name, ok := t.byID[id]; ok {
			return name
		}
	}
	return "Unknown city (" + id + ")"
}
