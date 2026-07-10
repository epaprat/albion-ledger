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
	defer a.mu.Unlock()
	a.setCurrentCityLocked(city)
}

// setCurrentCityLocked is SetCurrentCity's body under an already-held lock. It is
// also invoked from SetContainer when the city is inferred mid-session (event 163
// never fired), so an inferred city triggers the SAME backfill/migrate/evict heal
// as a real city-entry — otherwise earlier city-less tabs stay stranded.
func (a *Aggregator) setCurrentCityLocked(city string) {
	a.currentCity = city
	if city == "" {
		return
	}
	// Backfill (010): tabs recorded before the city was known stranded in a generic
	// city-less "Bank" group that can never merge with the K overview's city group
	// (live-seen twice). The city arriving NOW is where those tabs physically are —
	// a bank must be opened in its own city.
	for o, c := range a.bankOwnerCity {
		if c == "" {
			a.bankOwnerCity[o] = city
		}
	}
	migrated := false
	for _, c := range a.containers {
		if c.location == model.LocBank && c.city == "" {
			c.city = city
			migrated = true
		}
	}
	if migrated {
		// The generic "Bank" group's freshness belongs to the real city now, and any
		// K-summary tab overlapping a just-migrated PHYSICAL tab must yield (the
		// reverse-dedup can only match once the city is known).
		if ts, ok := a.citySeen[bankCityName("")]; ok {
			if ts > a.citySeen[city] {
				a.citySeen[city] = ts
			}
			delete(a.citySeen, bankCityName(""))
		}
		for guid, c := range a.containers {
			if c.location == model.LocBank && !c.summary && c.city == city {
				a.evictSummaryOverlapsLocked(city, c.tab, guid)
			}
		}
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
		a.bankOwnerCity[o] = a.currentCity // the bank we're at right now owns these tabs
	}
}

// SetContainer REPLACES a container's items from a full snapshot. A known bank-tab
// owner → location bank under the current city + tab name; otherwise inventory.

// evictSummaryOverlapsLocked removes K-summary containers covering the same
// (city, tab) as a PHYSICAL container — the reverse direction of the
// summary-yields-to-physical rule. Without it, a physical open AFTER a K browse
// left both containers alive and Summary double-counted the tab (010 review).
// Caller holds a.mu.
func (a *Aggregator) evictSummaryOverlapsLocked(city, tab, keepGUID string) {
	if city == "" {
		return // unmatched until the city is known; the backfill re-runs this
	}
	for guid, c := range a.containers {
		if guid == keepGUID || !c.summary || c.city != city || c.tab != tab {
			continue
		}
		for objID := range c.items {
			if a.objLoc[objID] == guid {
				delete(a.objLoc, objID)
			}
		}
		delete(a.containers, guid)
		for i, g := range a.order {
			if g == guid {
				a.order = append(a.order[:i], a.order[i+1:]...)
				break
			}
		}
	}
}

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
		city = a.currentCity // "" until a current city is known (US3)
		if city == "" {
			// Mid-session start: event 163 (city entry) never fired, so currentCity is
			// unknown and this physically-opened tab would land city-less ("Bank" ghost +
			// no dedup against its K summary → double-count, live-seen 2026-07-10). Derive
			// the city from the K overview: a summary with THIS tab name pins it when the
			// name is unique to one city. Once resolved, pin currentCity so the remaining
			// tabs — including names shared across cities (e.g. "Hammadde") — inherit it.
			if inferred := a.inferBankCityLocked(tab); inferred != "" {
				city = inferred
				a.setCurrentCityLocked(inferred) // full heal: migrate/evict any earlier city-less tabs
			}
		}
		a.bankOwnerCity[ownerGUID] = city // opening the tab pins its city (most reliable)
	}

	if loc == model.LocBank {
		a.evictSummaryOverlapsLocked(city, tab, containerGUID)
	}
	c := a.ensureContainer(containerGUID)
	c.summary = false // a physical snapshot claims this container outright
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
	// Physical-first, K-later (the reverse of inferBankCityLocked): the player opened
	// this bank physically BEFORE any overview, so its tabs are city-less — event 163
	// never fired and there was no summary yet to infer the city from (live-seen
	// 2026-07-10: double-count returned as 247 = 119 summary + 128 physical). This
	// summary now names the city; if a city-less physical carries the same tab name it
	// IS this bank, so adopt the city and run the full heal (migrate the open tabs +
	// evict overlaps). The physical-wins guard below then matches and this summary yields.
	if a.currentCity == "" {
		for _, other := range a.containers {
			if !other.summary && other.location == model.LocBank && other.city == "" &&
				other.tab == tabName && len(other.items) > 0 {
				a.setCurrentCityLocked(city)
				break
			}
		}
	}
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
// lastValuationRefresh picks a "now" for read-time re-valuation staleness checks:
// the newest lastSeen across containers (the aggregator has no clock of its own).
func (a *Aggregator) lastValuationRefresh() int64 {
	var maxSeen int64
	for _, c := range a.containers {
		if c.lastSeen > maxSeen {
			maxSeen = c.lastSeen
		}
	}
	if a.equipped != nil && a.equipped.lastSeen > maxSeen {
		maxSeen = a.equipped.lastSeen
	}
	return maxSeen
}

func (a *Aggregator) List() []model.HoldingItem {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []model.HoldingItem
	guids := make([]string, 0, len(a.containers))
	for g := range a.containers {
		guids = append(guids, g)
	}
	sort.Strings(guids)
	nowMS := int64(0)
	if len(a.containers) > 0 || a.equipped != nil {
		nowMS = a.lastValuationRefresh()
	}
	appendSorted := func(items map[int]model.HoldingItem, loc model.Location, city, group string) {
		rows := make([]model.HoldingItem, 0, len(items))
		for _, it := range items {
			// Re-resolve at READ time: prices can arrive AFTER a row was written
			// (market browse pricing a bank summary) and a container's city can be
			// backfilled late (mid-session city discovery) — write-time snapshots
			// froze both out of existing rows (live-seen 2026-07-05).
			it.Valuation = a.val.Value(it.Item.Index, it.Item.Quality, nowMS)
			it.Location, it.City, it.Group = loc, city, group
			rows = append(rows, it)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Item.DisplayName < rows[j].Item.DisplayName })
		out = append(out, rows...)
	}
	physical := a.physicalBankTabsLocked()
	for _, g := range guids {
		c := a.containers[g]
		if a.isDupSummaryLocked(c, physical) {
			continue // a physical container covers this tab — skip the summary (no duplicate rows)
		}
		cityName := c.city
		if c.location == model.LocBank {
			cityName = bankCityName(c.city)
		}
		appendSorted(c.items, c.location, cityName, c.tab)
	}
	if a.equipped != nil {
		appendSorted(a.equipped.items, model.LocEquipped, "", a.equipped.tab)
	}
	return out
}

// Summary returns the grand total, unvalued count, and the city → tab rollup
// (inventory group first, then cities by last-seen). Honest seen/stale per group
// (Principle X). Equipped is out of scope for this view (feature 004).
// physicalBankTabsLocked returns the "city\x00tab" set covered by a PHYSICAL bank container
// (object-based, from an actual open). A K-overview SUMMARY for the same tab is a duplicate
// of it — summing both double-counts the tab (010) — so read paths skip a summary when a
// physical peer exists. This read-time guard is robust even when the write-time
// reconciliation missed (e.g. the physical container's city was backfilled AFTER the K
// summary arrived, so evictSummaryOverlaps never matched — live-seen 2026-07-10).
func (a *Aggregator) physicalBankTabsLocked() map[string]bool {
	set := map[string]bool{}
	for _, c := range a.containers {
		if !c.summary && c.location == model.LocBank && len(c.items) > 0 {
			set[bankCityName(c.city)+"\x00"+c.tab] = true
		}
	}
	return set
}

// isDupSummaryLocked reports whether c is a K summary whose (city,tab) a physical already covers.
func (a *Aggregator) isDupSummaryLocked(c *container, physical map[string]bool) bool {
	return c.summary && c.location == model.LocBank && physical[bankCityName(c.city)+"\x00"+c.tab]
}

// inferBankCityLocked derives the current bank's city from the K overview when a tab
// opens before the city is known (mid-session start, event 163 never fired). A K
// SUMMARY carrying this tab name pins the city ONLY when the name is unique to one
// city — ambiguous names shared across cities (e.g. "Hammadde") return "" and wait
// for an unambiguous tab in the same physical open to resolve currentCity first.
func (a *Aggregator) inferBankCityLocked(tab string) string {
	found := ""
	for _, c := range a.containers {
		if !c.summary || c.location != model.LocBank || c.tab != tab || c.city == "" {
			continue
		}
		if found != "" && found != c.city {
			return "" // same tab name in two cities — cannot disambiguate
		}
		found = c.city
	}
	return found
}

func (a *Aggregator) Summary(nowMS int64) model.HoldingsSummary {
	a.mu.Lock()
	defer a.mu.Unlock()
	physical := a.physicalBankTabsLocked()

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
		if a.isDupSummaryLocked(c, physical) {
			continue // a physical container covers this tab — skip the summary (no double-count)
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
			// Read-time re-valuation (see List): prices arriving after the write
			// must reflect in totals too.
			v := a.val.Value(it.Item.Index, it.Item.Quality, nowMS)
			if v.Source == model.SourceUnknown {
				t.unvalued++
			} else {
				t.subtotal += v.Amount * int64(it.Count) // stack value
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
	var gameEst int64
	for _, v := range a.cityVaultValue {
		gameEst += v
	}
	return model.HoldingsSummary{TotalValue: grand, GameEstTotal: gameEst, UnvaluedCount: grandUnvalued, Cities: out}
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

// ── Persistence (020): snapshot for the store + hydrate on startup ───────────

// ContainerSnapshot is one persisted holdings container: its stable id, metadata, and
// items — enough to restore the container on the next launch. Its LastSeen carries the
// original observation time, so the freshness layer (008) renders it stale until it is
// re-seen live.
type ContainerSnapshot struct {
	GUID     string
	Location model.Location
	City     string
	Tab      string
	LastSeen int64
	Pinned   bool
	Items    []model.HoldingItem
}

// Snapshot returns a read-only copy of every tracked container for persistence (020).
func (a *Aggregator) Snapshot() []ContainerSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]ContainerSnapshot, 0, len(a.containers))
	for guid, c := range a.containers {
		// A PHYSICAL bank container is keyed by an EPHEMERAL wire guid that never recurs
		// (010): persisting it would hydrate as stale junk next launch (a physical peer that
		// then wrongly wins the read-time dedup over a fresh K summary). Only the stable-id
		// K summaries + the self bag/equipped are persisted; the physical view is re-observed
		// live when the player opens the bank (020 live-test fix).
		if c.location == model.LocBank && !c.summary {
			continue
		}
		items := make([]model.HoldingItem, 0, len(c.items))
		for _, it := range c.items {
			items = append(items, it)
		}
		out = append(out, ContainerSnapshot{
			GUID: guid, Location: c.location, City: c.city, Tab: c.tab,
			LastSeen: c.lastSeen, Pinned: c.pinned, Items: items,
		})
	}
	return out
}

// SeedContainers restores persisted containers at startup (020). A container that LIVE data
// already claimed this session (observed: lastSeen>0 or holding items) is NEVER overwritten.
// A container that merely EXISTS but is unobserved — e.g. the self bag/equipped that
// EnsureSelfContainer pre-creates empty at startup — IS filled from persistence, so the
// inventory/equipped hydrate too (fix: they were skipped by a bare "exists → continue").
// The restored LastSeen keeps the freshness/stale labelling honest until a live re-observe.
func (a *Aggregator) SeedContainers(snaps []ContainerSnapshot) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, sn := range snaps {
		if c, exists := a.containers[sn.GUID]; exists && (c.lastSeen > 0 || len(c.items) > 0) {
			continue // live data already claimed this container — do not clobber it
		}
		c := a.ensureContainer(sn.GUID) // returns the pre-created empty one, or a fresh bounded slot
		c.location, c.city, c.tab, c.lastSeen = sn.Location, sn.City, sn.Tab, sn.LastSeen
		c.pinned = c.pinned || sn.Pinned // keep a pre-created self container pinned
		c.items = make(map[int]model.HoldingItem, len(sn.Items))
		for _, it := range sn.Items {
			c.items[it.ObjID] = it
			if it.ObjID != 0 {
				a.objLoc[it.ObjID] = sn.GUID
			}
		}
	}
}
