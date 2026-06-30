// Package holdings aggregates the player's held items (inventory/bank) into a
// deduped, valued, bounded view, grouped by container (inventory vs each bank
// tab). A container's items are REPLACED on each update (not appended) so item
// moves never duplicate and memory stays bounded (FR-010, Principle XI).
package holdings

import (
	"sort"
	"strings"
	"sync"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/port"
)

// ItemRef is a resolved container slot: item type index + quality.
type ItemRef struct{ Index, Quality int }

type container struct {
	location model.Location
	group    string
	items    []model.HoldingItem
	lastSeen int64
}

// Aggregator holds the live holdings state.
type Aggregator struct {
	cat        port.Catalog
	val        port.Valuer
	staleAfter int64

	mu         sync.Mutex
	containers map[string]*container // keyed by container GUID (one per tab/inventory)
	equipped   *container
	bankOwners map[string]string         // owner GUID -> tab name (from BankVaultInfo)
	sections   map[model.Location]int64  // last-seen ms per location
}

// New creates an Aggregator.
func New(cat port.Catalog, val port.Valuer, staleAfter int64) *Aggregator {
	return &Aggregator{
		cat: cat, val: val, staleAfter: staleAfter,
		containers: map[string]*container{},
		bankOwners: map[string]string{},
		sections:   map[model.Location]int64{},
	}
}

// SetBankVault records the bank's tab owner GUIDs and names (from BankVaultInfo).
func (a *Aggregator) SetBankVault(owners, tabNames []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, o := range owners {
		name := ""
		if i < len(tabNames) {
			name = friendlyTab(tabNames[i])
		}
		a.bankOwners[o] = name
	}
}

// SetContainer REPLACES a container's items, resolving its group from the owner:
// a known bank-tab owner → "Bank · <tab>"; otherwise the player's "Inventory".
func (a *Aggregator) SetContainer(containerGUID, ownerGUID string, refs []ItemRef, nowMS int64) model.Location {
	a.mu.Lock()
	defer a.mu.Unlock()

	loc := model.LocInventory
	group := "Inventory"
	if tab, isBank := a.bankOwners[ownerGUID]; isBank {
		loc = model.LocBank
		group = "Bank"
		if tab != "" {
			group = "Bank · " + tab
		}
	}

	items := make([]model.HoldingItem, 0, len(refs))
	for _, r := range refs {
		items = append(items, a.row(r.Index, r.Quality, loc, group, nowMS))
	}
	a.containers[containerGUID] = &container{location: loc, group: group, items: items, lastSeen: nowMS}
	a.sections[loc] = nowMS
	return loc
}

// SetEquipped REPLACES the equipped set.
func (a *Aggregator) SetEquipped(items []ItemRef, nowMS int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rows := make([]model.HoldingItem, 0, len(items))
	for _, e := range items {
		rows = append(rows, a.row(e.Index, e.Quality, model.LocEquipped, "Equipped", nowMS))
	}
	a.equipped = &container{location: model.LocEquipped, group: "Equipped", items: rows, lastSeen: nowMS}
	a.sections[model.LocEquipped] = nowMS
}

func (a *Aggregator) row(index, quality int, loc model.Location, group string, nowMS int64) model.HoldingItem {
	return model.HoldingItem{
		Item:      a.cat.Resolve(index, quality),
		Valuation: a.val.Value(index, quality, nowMS),
		Location:  loc,
		Group:     group,
		Count:     1,
		LastSeen:  nowMS,
	}
}

// List returns all held items (containers + equipped), ordered by group.
func (a *Aggregator) List() []model.HoldingItem {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []model.HoldingItem
	guids := make([]string, 0, len(a.containers))
	for g := range a.containers {
		guids = append(guids, g)
	}
	sort.Strings(guids)
	for _, g := range guids {
		out = append(out, a.containers[g].items...)
	}
	if a.equipped != nil {
		out = append(out, a.equipped.items...)
	}
	return out
}

// Summary returns the total known value, unvalued count, and per-location state.
func (a *Aggregator) Summary(nowMS int64) model.HoldingsSummary {
	a.mu.Lock()
	defer a.mu.Unlock()

	var total int64
	unvalued := 0
	count := func(items []model.HoldingItem) {
		for _, it := range items {
			if it.Valuation.Source == model.SourceUnknown {
				unvalued++
			} else {
				total += it.Valuation.Amount
			}
		}
	}
	for _, c := range a.containers {
		count(c.items)
	}
	if a.equipped != nil {
		count(a.equipped.items)
	}

	sections := map[model.Location]model.SectionState{}
	for _, loc := range []model.Location{model.LocInventory, model.LocBank, model.LocEquipped, model.LocHoldings} {
		last, seen := a.sections[loc]
		sections[loc] = model.SectionState{
			Seen:     seen,
			LastSeen: last,
			Stale:    seen && nowMS-last > a.staleAfter,
		}
	}
	return model.HoldingsSummary{TotalValue: total, UnvaluedCount: unvalued, Sections: sections}
}

// friendlyTab turns a raw tab name into a readable one (loc keys like
// "@BUILDINGS_T1_BANK" become "Main").
func friendlyTab(name string) string {
	if name == "" || strings.HasPrefix(name, "@") {
		return "Main"
	}
	return name
}
