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

// ItemRef is a resolved container slot: item type index + quality + stack count.
type ItemRef struct {
	Index, Quality int
	Count          int // stack quantity (1 for non-stackables); 0 is treated as 1
}

// SlotItem pairs a container slot's in-world object id with its resolved ref, so
// incremental moves (Put/Delete by object id) can target individual items.
type SlotItem struct {
	ObjID int
	Ref   ItemRef
}

type container struct {
	location model.Location
	city     string                    // city display name; "" for inventory (or generic bank until a city is known)
	tab      string                    // bank tab name, or "Inventory"
	items    map[int]model.HoldingItem // objId -> row
	lastSeen int64
	pinned   bool // player's own containers: never cap-evicted (a frozen bag is a defect)
	summary  bool // K-overview type-based summary tab (010) — yields to physical data
}

// containerCap bounds the number of tracked containers (Principle XI; FR-011).
const containerCap = 512

// Aggregator holds the live holdings state.
type Aggregator struct {
	cat        port.Catalog
	val        port.Valuer
	staleAfter int64

	mu         sync.Mutex
	containers map[string]*container // keyed by container GUID (one per tab/inventory)
	order      []string              // insertion order for bounded eviction
	objLoc     map[int]string        // objId -> containerGUID (for incremental move/delete)
	equipped   *container
	// bankOwners/bankOwnerCity/citySeen are keyed by a game-fixed set (bank-tab owner
	// GUIDs, city names) that is tiny and bounded in practice — no eviction needed (XI).
	bankOwners    map[string]string // owner GUID -> tab name (from BankVaultInfo)
	bankOwnerCity map[string]string // owner GUID -> city it belongs to (current city at 414 time)
	currentCity   string            // latest observed city display name; "" if unknown
	citySeen      map[string]int64  // city display name -> last-seen ms
	// cityVaultValue: K-overview per-city vault totals (010); replaced wholesale
	// each R:516, bounded by the game's location count.
	cityVaultValue map[string]int64
}

// New creates an Aggregator.
func New(cat port.Catalog, val port.Valuer, staleAfter int64) *Aggregator {
	return &Aggregator{
		cat: cat, val: val, staleAfter: staleAfter,
		containers:     map[string]*container{},
		objLoc:         map[int]string{},
		bankOwners:     map[string]string{},
		bankOwnerCity:  map[string]string{},
		citySeen:       map[string]int64{},
		cityVaultValue: map[string]int64{},
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
		a.bankOwnerCity[o] = a.currentCity // the bank we're at right now owns these tabs
	}
}

// SetContainer REPLACES a container's items from a full snapshot. A known bank-tab
// owner → location bank under the current city + tab name; otherwise inventory.
func (a *Aggregator) SetContainer(containerGUID, ownerGUID string, slots []SlotItem, nowMS int64) model.Location {
	a.mu.Lock()
	defer a.mu.Unlock()

	loc := model.LocInventory
	tab := "Bag"
	city := ""
	if tabName, isBank := a.bankOwners[ownerGUID]; isBank {
		loc = model.LocBank
		tab = "Bank"
		if tabName != "" {
			tab = tabName
		}
		city = a.currentCity              // "" until a current city is known (US3)
		a.bankOwnerCity[ownerGUID] = city // opening the tab pins its city (most reliable)
	}

	c := a.ensureContainer(containerGUID)
	// Drop this container's own old index entries (only those still pointing here, so
	// we never steal an entry an object earned by moving to another container).
	for objID := range c.items {
		if a.objLoc[objID] == containerGUID {
			delete(a.objLoc, objID)
		}
	}
	c.location, c.city, c.tab, c.lastSeen = loc, city, tab, nowMS
	c.items = make(map[int]model.HoldingItem, len(slots))
	for _, s := range slots {
		a.removeObj(s.ObjID) // if this object still lived in another container, drop it there first
		row := a.row(s.Ref.Index, s.Ref.Quality, s.Ref.Count, loc, city, tab, nowMS)
		row.ObjID = s.ObjID
		c.items[s.ObjID] = row
		a.objLoc[s.ObjID] = containerGUID
	}
	a.touchCity(loc, city, nowMS)
	return loc
}

// SetSelfContainer REPLACES a player-owned container (bag / equipped) that has no
// owner GUID in the wire data — it comes from own-state slot arrays. Grouped under
// the inventory city with the given tab (e.g. "Bag", "Equipped").
// EnsureSelfContainer pre-creates (or re-labels) a player-owned container WITHOUT
// marking anything as observed: no citySeen touch, lastSeen stays zero until real
// wire data arrives, and the container is pinned against cap eviction. Lets live puts
// bridged to the player's containers land before the first own-state snapshot while
// keeping the UI's "nothing captured yet" state honest (008).
func (a *Aggregator) EnsureSelfContainer(containerGUID, tab string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.ensureContainer(containerGUID)
	c.location, c.city, c.tab, c.pinned = model.LocInventory, "", tab, true
}

func (a *Aggregator) SetSelfContainer(containerGUID, tab string, slots []SlotItem, nowMS int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.ensureContainer(containerGUID)
	c.pinned = true
	for objID := range c.items {
		if a.objLoc[objID] == containerGUID {
			delete(a.objLoc, objID)
		}
	}
	c.location, c.city, c.tab, c.lastSeen = model.LocInventory, "", tab, nowMS
	c.items = make(map[int]model.HoldingItem, len(slots))
	for _, s := range slots {
		a.removeObj(s.ObjID)
		row := a.row(s.Ref.Index, s.Ref.Quality, s.Ref.Count, model.LocInventory, "", tab, nowMS)
		row.ObjID = s.ObjID
		c.items[s.ObjID] = row
		a.objLoc[s.ObjID] = containerGUID
	}
	a.citySeen[invName] = nowMS
}

// PutItem incrementally adds (or moves) one item into a KNOWN container —
// InventoryPutItem / applied move. The item is removed from any previous container
// first (a move), then placed here. Puts into an unknown container GUID are NOT
// applied (applied=false): the caller decides the fallback (drop from view) — the
// old "first-seen container defaults to Bag" guess glued phantom 'Bag' tabs onto
// bank/market GUIDs (008). Returning the outcome keeps "item entered untracked
// space" observable instead of a silent no-op that leaves the item stale in place.
func (a *Aggregator) PutItem(containerGUID string, objID int, ref ItemRef, nowMS int64) (applied bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, known := a.containers[containerGUID]
	if !known {
		return false
	}
	a.removeObj(objID) // a move: drop from wherever it was
	row := a.row(ref.Index, ref.Quality, ref.Count, c.location, c.city, c.tab, nowMS)
	row.ObjID = objID
	c.items[objID] = row
	c.lastSeen = nowMS
	a.objLoc[objID] = containerGUID
	a.touchCity(c.location, c.city, nowMS)
	return true
}

// DeleteItem incrementally removes one item by object id — InventoryDeleteItem.
func (a *Aggregator) DeleteItem(objID int, nowMS int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if g, ok := a.objLoc[objID]; ok {
		if c := a.containers[g]; c != nil {
			c.lastSeen = nowMS
		}
	}
	a.removeObj(objID)
}

// ensureContainer returns the container for a GUID, creating an empty one (bounded)
// if absent. New containers carry empty metadata until a Put/snapshot fills it.
func (a *Aggregator) ensureContainer(guid string) *container {
	if c, ok := a.containers[guid]; ok {
		return c
	}
	if len(a.containers) >= containerCap {
		// Evict the oldest UNPINNED container: the player's own bag/equipped sit at
		// the head of the order (pre-created at startup) and evicting them would
		// silently freeze the bag view for the rest of the session (008 review).
		for i, old := range a.order {
			victim := a.containers[old]
			if victim == nil || victim.pinned {
				continue
			}
			a.order = append(a.order[:i], a.order[i+1:]...)
			for objID := range victim.items {
				if a.objLoc[objID] == old { // don't drop an entry an object earned elsewhere
					delete(a.objLoc, objID)
				}
			}
			delete(a.containers, old)
			break
		}
	}
	a.order = append(a.order, guid)
	c := &container{items: map[int]model.HoldingItem{}}
	a.containers[guid] = c
	return c
}

// removeObj drops an object id from its container and the index.
func (a *Aggregator) removeObj(objID int) {
	if g, ok := a.objLoc[objID]; ok {
		if c := a.containers[g]; c != nil {
			delete(c.items, objID)
		}
		delete(a.objLoc, objID)
	}
}

func (a *Aggregator) touchCity(loc model.Location, city string, nowMS int64) {
	if loc == model.LocBank {
		a.citySeen[bankCityName(city)] = nowMS
	} else {
		a.citySeen[invName] = nowMS
	}
}

// invName is the display name of the inventory pseudo-city group.
const invName = "Inventory"

// SetVaultSummaryTab REPLACES one bank tab from a K bank-overview content summary
// (feature 010): city-tagged, REAL tab name, type-based rows. Row keys are SYNTHETIC
// NEGATIVE ids (-(index*16+quality)-1) — real object ids are positive, so summary
// rows can never collide with tracked objects or enter the 008 move/delete paths.
// The same tab guid seen via a physical open (SetContainer/99) shares this container:
// last writer wins, content AND name (contract rule 5).
func (a *Aggregator) SetVaultSummaryTab(tabGUID, city, tabName string, rows []ItemRef, nowMS int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// A PHYSICAL container (object-based, from an actual bank open) already covering
	// this (city, tab) always wins: it is richer (object ids, declared values, real
	// qualities), and summing both sources would double-count the tab (live-seen
	// 2026-07-05: the same bank showed under two city groups). The K summary only
	// fills tabs the player has never physically opened.
	for guid, other := range a.containers {
		if guid != tabGUID && !other.summary && other.location == model.LocBank &&
			other.city == city && other.tab == tabName && len(other.items) > 0 {
			return
		}
	}
	c := a.ensureContainer(tabGUID)
	c.summary = true
	for objID := range c.items {
		if a.objLoc[objID] == tabGUID {
			delete(a.objLoc, objID)
		}
	}
	c.location, c.city, c.tab = model.LocBank, city, tabName
	c.items = make(map[int]model.HoldingItem, len(rows))
	c.lastSeen = nowMS
	for _, r := range rows {
		synthID := -(r.Index*16 + r.Quality) - 1
		row := a.row(r.Index, r.Quality, r.Count, model.LocBank, city, tabName, nowMS)
		row.ObjID = synthID
		c.items[synthID] = row
	}
	a.touchCity(model.LocBank, city, nowMS)
}

// SetCityVaultValues REPLACES the game-reported per-city vault totals from the K
// overview location list (R:516 k5, already scaled to silver by the caller). A fresh
// K opening is the authority: stale cities drop wholesale (XI — no accretion).
func (a *Aggregator) SetCityVaultValues(values map[string]int64, nowMS int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cityVaultValue = make(map[string]int64, len(values))
	for city, v := range values {
		a.cityVaultValue[city] = v
		a.citySeen[bankCityName(city)] = nowMS
	}
}

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
	rows := make(map[int]model.HoldingItem, len(items))
	for i, e := range items {
		rows[i] = a.row(e.Index, e.Quality, e.Count, model.LocEquipped, "", "Equipped", nowMS)
	}
	a.equipped = &container{location: model.LocEquipped, city: "", tab: "Equipped", items: rows, lastSeen: nowMS}
}

func (a *Aggregator) row(index, quality, count int, loc model.Location, city, group string, nowMS int64) model.HoldingItem {
	if count < 1 {
		count = 1
	}
	return model.HoldingItem{
		Item:      a.cat.Resolve(index, quality),
		Valuation: a.val.Value(index, quality, nowMS),
		Location:  loc,
		City:      city,
		Group:     group,
		Count:     count,
		LastSeen:  nowMS,
	}
}

// List returns all held items (containers + equipped). Items within a container are
// ordered by display name for a stable view.
func (a *Aggregator) List() []model.HoldingItem {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []model.HoldingItem
	guids := make([]string, 0, len(a.containers))
	for g := range a.containers {
		guids = append(guids, g)
	}
	sort.Strings(guids)
	appendSorted := func(items map[int]model.HoldingItem) {
		rows := make([]model.HoldingItem, 0, len(items))
		for _, it := range items {
			rows = append(rows, it)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Item.DisplayName < rows[j].Item.DisplayName })
		out = append(out, rows...)
	}
	for _, g := range guids {
		appendSorted(a.containers[g].items)
	}
	if a.equipped != nil {
		appendSorted(a.equipped.items)
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
		if c.lastSeen == 0 && len(c.items) == 0 {
			continue // pre-created, never observed: showing it would fake freshness (X/XII)
		}
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
				t.subtotal += it.Valuation.Amount * int64(it.Count) // stack value
			}
		}
	}

	// Known-but-unopened bank tabs (named via BankVaultInfo, not yet observed) are
	// listed under THEIR OWN city (recorded at 414 time), not the current one, so a
	// tab never shows under the wrong city (FR-003/FR-004).
	for owner, tabName := range a.bankOwners {
		ca := getCity(bankCityName(a.bankOwnerCity[owner]), false)
		name := tabName
		if name == "" {
			name = "Bank"
		}
		if _, ok := ca.tabs[name]; !ok {
			ca.tabs[name] = &tabAcc{opened: false}
		}
	}

	// Cities known only from the K-overview vault totals (010) still get a group:
	// the user should see "Thetford: 11M in the vault" before any tab is browsed.
	for city := range a.cityVaultValue {
		getCity(bankCityName(city), false)
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
			VaultValue: a.cityVaultValue[cn], // 0 = not reported (010)
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
