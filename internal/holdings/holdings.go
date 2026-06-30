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
	city     string // city display name; "" for inventory (or generic bank until a city is known)
	tab      string // bank tab name, or "Inventory"
	items    []model.HoldingItem
	lastSeen int64
}

// containerCap bounds the number of tracked containers (Principle XI; FR-011).
const containerCap = 512

// Aggregator holds the live holdings state.
type Aggregator struct {
	cat        port.Catalog
	val        port.Valuer
	staleAfter int64

	mu          sync.Mutex
	containers  map[string]*container // keyed by container GUID (one per tab/inventory)
	order       []string              // insertion order for bounded eviction
	equipped    *container
	bankOwners  map[string]string // owner GUID -> tab name (from BankVaultInfo)
	currentCity string            // latest observed city display name; "" if unknown
	citySeen    map[string]int64  // city display name -> last-seen ms
}

// New creates an Aggregator.
func New(cat port.Catalog, val port.Valuer, staleAfter int64) *Aggregator {
	return &Aggregator{
		cat: cat, val: val, staleAfter: staleAfter,
		containers: map[string]*container{},
		bankOwners: map[string]string{},
		citySeen:   map[string]int64{},
	}
}

// SetCurrentCity records the player's current city (display name). Subsequent bank
// containers are grouped under it. "" leaves the city unknown.
func (a *Aggregator) SetCurrentCity(city string) {
	a.mu.Lock()
	a.currentCity = city
	a.mu.Unlock()
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

// SetContainer REPLACES a container's items. A known bank-tab owner → location bank,
// grouped under the current city + its tab name; otherwise the player's inventory.
func (a *Aggregator) SetContainer(containerGUID, ownerGUID string, refs []ItemRef, nowMS int64) model.Location {
	a.mu.Lock()
	defer a.mu.Unlock()

	loc := model.LocInventory
	tab := "Inventory"
	city := ""
	if tabName, isBank := a.bankOwners[ownerGUID]; isBank {
		loc = model.LocBank
		tab = "Bank"
		if tabName != "" {
			tab = tabName
		}
		city = a.currentCity // "" until a current city is known (US3)
	}

	items := make([]model.HoldingItem, 0, len(refs))
	for _, r := range refs {
		items = append(items, a.row(r.Index, r.Quality, loc, city, tab, nowMS))
	}
	if _, exists := a.containers[containerGUID]; !exists {
		if len(a.containers) >= containerCap && len(a.order) > 0 {
			delete(a.containers, a.order[0])
			a.order = a.order[1:]
		}
		a.order = append(a.order, containerGUID)
	}
	a.containers[containerGUID] = &container{location: loc, city: city, tab: tab, items: items, lastSeen: nowMS}
	if loc == model.LocBank {
		a.citySeen[bankCityName(city)] = nowMS
	} else {
		a.citySeen[invName] = nowMS
	}
	return loc
}

// invName is the display name of the inventory pseudo-city group.
const invName = "Inventory"

// bankCityName is the display name for a bank container's city group; an unknown
// city ("") groups under a generic "Bank" until a current city is known (US3).
func bankCityName(city string) string {
	if city == "" {
		return "Bank"
	}
	return city
}

// SetEquipped REPLACES the equipped set.
func (a *Aggregator) SetEquipped(items []ItemRef, nowMS int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rows := make([]model.HoldingItem, 0, len(items))
	for _, e := range items {
		rows = append(rows, a.row(e.Index, e.Quality, model.LocEquipped, "", "Equipped", nowMS))
	}
	a.equipped = &container{location: model.LocEquipped, city: "", tab: "Equipped", items: rows, lastSeen: nowMS}
}

func (a *Aggregator) row(index, quality int, loc model.Location, city, group string, nowMS int64) model.HoldingItem {
	return model.HoldingItem{
		Item:      a.cat.Resolve(index, quality),
		Valuation: a.val.Value(index, quality, nowMS),
		Location:  loc,
		City:      city,
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

// Summary returns the grand total, unvalued count, and the city → tab rollup
// (inventory group first, then cities by last-seen). Honest seen/stale per group
// (Principle X). Equipped is out of scope for this view (feature 004).
func (a *Aggregator) Summary(nowMS int64) model.HoldingsSummary {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Build per-city → per-tab accumulators from observed containers.
	type tabAcc struct {
		count, unvalued int
		subtotal        int64
		lastSeen        int64
		opened          bool
	}
	type cityAcc struct {
		isInv bool
		tabs  map[string]*tabAcc
	}
	cities := map[string]*cityAcc{}
	getCity := func(name string, isInv bool) *cityAcc {
		c, ok := cities[name]
		if !ok {
			c = &cityAcc{isInv: isInv, tabs: map[string]*tabAcc{}}
			cities[name] = c
		}
		return c
	}

	for _, c := range a.containers {
		cityName := invName
		isInv := true
		if c.location == model.LocBank {
			cityName, isInv = bankCityName(c.city), false
		}
		ca := getCity(cityName, isInv)
		t, ok := ca.tabs[c.tab]
		if !ok {
			t = &tabAcc{}
			ca.tabs[c.tab] = t
		}
		t.opened = true
		if c.lastSeen > t.lastSeen {
			t.lastSeen = c.lastSeen
		}
		for _, it := range c.items {
			t.count++
			if it.Valuation.Source == model.SourceUnknown {
				t.unvalued++
			} else {
				t.subtotal += it.Valuation.Amount
			}
		}
	}

	// Known-but-unopened bank tabs (named via BankVaultInfo, not yet observed) are
	// listed under the current city group so the user sees they exist (FR-004).
	if len(a.bankOwners) > 0 {
		ca := getCity(bankCityName(a.currentCity), false)
		for _, tabName := range a.bankOwners {
			name := tabName
			if name == "" {
				name = "Bank"
			}
			if _, ok := ca.tabs[name]; !ok {
				ca.tabs[name] = &tabAcc{opened: false}
			}
		}
	}

	// Roll up into ordered DTOs.
	var grand int64
	var grandUnvalued int
	cityNames := make([]string, 0, len(cities))
	for name := range cities {
		cityNames = append(cityNames, name)
	}
	sort.Slice(cityNames, func(i, j int) bool {
		// inventory first, then by last-seen desc, then name.
		if cityNames[i] == invName {
			return true
		}
		if cityNames[j] == invName {
			return false
		}
		li, lj := a.citySeen[cityNames[i]], a.citySeen[cityNames[j]]
		if li != lj {
			return li > lj
		}
		return cityNames[i] < cityNames[j]
	})

	out := make([]model.CitySummary, 0, len(cityNames))
	for _, cn := range cityNames {
		ca := cities[cn]
		tabNames := make([]string, 0, len(ca.tabs))
		for tn := range ca.tabs {
			tabNames = append(tabNames, tn)
		}
		sort.Strings(tabNames)
		var cityTotal int64
		var cityUnvalued int
		tabs := make([]model.TabSummary, 0, len(tabNames))
		for _, tn := range tabNames {
			t := ca.tabs[tn]
			cityTotal += t.subtotal
			cityUnvalued += t.unvalued
			tabs = append(tabs, model.TabSummary{
				Name: tn, ItemCount: t.count, Subtotal: t.subtotal,
				UnvaluedCount: t.unvalued, Opened: t.opened,
				State: a.stateOf(t.lastSeen, t.opened, nowMS),
			})
		}
		grand += cityTotal
		grandUnvalued += cityUnvalued
		out = append(out, model.CitySummary{
			Name: cn, IsInventory: ca.isInv, Total: cityTotal, UnvaluedCount: cityUnvalued,
			Tabs: tabs, State: a.stateOf(a.citySeen[cn], a.citySeen[cn] > 0, nowMS),
		})
	}
	return model.HoldingsSummary{TotalValue: grand, UnvaluedCount: grandUnvalued, Cities: out}
}

// stateOf builds a SectionState from a last-seen ms (seen=false when not observed).
func (a *Aggregator) stateOf(lastSeen int64, seen bool, nowMS int64) model.SectionState {
	return model.SectionState{
		Seen:     seen,
		LastSeen: lastSeen,
		Stale:    seen && nowMS-lastSeen > a.staleAfter,
	}
}

// friendlyTab turns a raw tab name into a readable one (loc keys like
// "@BUILDINGS_T1_BANK" become "Main").
func friendlyTab(name string) string {
	if name == "" || strings.HasPrefix(name, "@") {
		return "Main"
	}
	return name
}
