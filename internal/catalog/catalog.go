// Package catalog resolves numeric item indexes (as seen in packets) to item
// identities, using a bundled/override data file. Names and ids change with game
// patches, so the catalog is hot-swappable at runtime (FR-012).
package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

type fileFormat struct {
	Items []model.CatalogEntry `json:"items"`
}

// Catalog is a concurrent-safe item index → identity resolver.
type Catalog struct {
	mu      sync.RWMutex
	byIndex map[int]model.CatalogEntry
	byName  map[string]int // uniqueName → index (market prices arrive by name, 010)
}

// New builds a catalog from JSON bytes (e.g. the embedded default).
func New(jsonBytes []byte) (*Catalog, error) {
	idx, err := parse(jsonBytes)
	if err != nil {
		return nil, err
	}
	return &Catalog{byIndex: idx, byName: nameIndex(idx)}, nil
}

func nameIndex(idx map[int]model.CatalogEntry) map[string]int {
	names := make(map[string]int, len(idx))
	for i, e := range idx {
		if e.UniqueName != "" {
			names[e.UniqueName] = i
		}
	}
	return names
}

func parse(jsonBytes []byte) (map[int]model.CatalogEntry, error) {
	var f fileFormat
	if err := json.Unmarshal(jsonBytes, &f); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	idx := make(map[int]model.CatalogEntry, len(f.Items))
	for _, it := range f.Items {
		idx[it.Index] = it
	}
	return idx, nil
}

// Resolve returns the item identity for a numeric index and quality. Never errors;
// an unknown index yields a safe placeholder (FR-007).
func (c *Catalog) Resolve(index int, quality int) model.Item {
	c.mu.RLock()
	entry, ok := c.byIndex[index]
	c.mu.RUnlock()

	if !ok {
		return model.Item{
			Index:       index,
			DisplayName: "Unknown item #" + strconv.Itoa(index),
			Quality:     quality,
			Known:       false,
		}
	}
	tier, enchant := tierEnchant(entry.UniqueName)
	return model.Item{
		Index:       index,
		DisplayName: entry.Name,
		UniqueName:  entry.UniqueName,
		Tier:        tier,
		Enchant:     enchant,
		Quality:     quality,
		Known:       true,
	}
}

// Reload swaps in a new catalog file; a malformed file is rejected and the
// previous catalog is kept (FR-012, Principle IV).
func (c *Catalog) Reload(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	idx, err := parse(b)
	if err != nil {
		return err // keep previous
	}
	c.mu.Lock()
	c.byIndex = idx
	c.byName = nameIndex(idx)
	c.mu.Unlock()
	return nil
}

// tierEnchant parses tier (T<n>_...) and enchant (@<n>) from a unique name.
func tierEnchant(uniqueName string) (tier, enchant int) {
	if len(uniqueName) >= 2 && (uniqueName[0] == 'T' || uniqueName[0] == 't') {
		if n, err := strconv.Atoi(string(uniqueName[1])); err == nil {
			tier = n
		}
	}
	if i := strings.LastIndexByte(uniqueName, '@'); i >= 0 && i+1 < len(uniqueName) {
		if n, err := strconv.Atoi(uniqueName[i+1:]); err == nil {
			enchant = n
		}
	}
	return tier, enchant
}

// IndexOf resolves a uniqueName (e.g. "T4_RUNE@1") to its catalog index — market
// order feeds identify items by name, valuation by index (feature 010).
func (c *Catalog) IndexOf(uniqueName string) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	idx, ok := c.byName[uniqueName]
	return idx, ok
}
