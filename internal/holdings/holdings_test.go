package holdings

import (
	"fmt"
	"testing"

	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

const cat = `{"items":[
  {"index":920,"uniqueName":"T7_WOOD","name":"Ashenbark Logs"},
  {"index":837,"uniqueName":"T8_MEAL","name":"Avalonian Stew"}
]}`

// objCounter gives every test slot a globally unique object id, so two containers
// never accidentally share one (which the aggregator correctly treats as a move).
var objCounter int

// slots wraps item indices into SlotItem with distinct object ids.
func slots(indices ...int) []SlotItem {
	s := make([]SlotItem, len(indices))
	for i, idx := range indices {
		objCounter++
		s[i] = SlotItem{ObjID: objCounter, Ref: ItemRef{Index: idx}}
	}
	return s
}

func newAgg(t *testing.T) (*Aggregator, *valuation.Book) {
	t.Helper()
	c, err := catalog.New([]byte(cat))
	if err != nil {
		t.Fatal(err)
	}
	book := valuation.NewBook()
	return New(c, valuation.NewValuer(book, model.DefaultStaleAfterMS), model.DefaultStaleAfterMS), book
}

func TestContainerReplaceNoDuplicate(t *testing.T) {
	a, _ := newAgg(t)
	a.SetContainer("c1", "owner", slots(920, 837), 1000)
	if len(a.List()) != 2 {
		t.Fatalf("want 2 items, got %d", len(a.List()))
	}
	a.SetContainer("c1", "owner", slots(920), 1100) // item moved out → REPLACE
	if n := len(a.List()); n != 1 {
		t.Fatalf("after replace want 1, got %d (duplicate/append bug)", n)
	}
}

func TestInventoryVsBankByOwner(t *testing.T) {
	a, _ := newAgg(t)
	// Unknown owner → Inventory.
	a.SetContainer("c1", "playerOwner", slots(920), 1000)
	if got := a.List()[0]; got.Location != model.LocInventory || got.Group != "Bag" {
		t.Fatalf("unmatched owner should be Bag/inventory, got loc=%s group=%q", got.Location, got.Group)
	}
	// Owner declared as a bank tab → location bank, tab name as group.
	a.SetBankVault([]string{"bankOwner"}, []string{"Items"})
	a.SetContainer("c2", "bankOwner", slots(837), 1100)
	var bankRow model.HoldingItem
	for _, r := range a.List() {
		if r.Item.Index == 837 {
			bankRow = r
		}
	}
	if bankRow.Location != model.LocBank || bankRow.Group != "Items" {
		t.Fatalf("bank-owner container should be tab 'Items', got loc=%s group=%q", bankRow.Location, bankRow.Group)
	}
}

func TestFriendlyTab(t *testing.T) {
	a, _ := newAgg(t)
	a.SetBankVault([]string{"o"}, []string{"@BUILDINGS_T1_BANK"})
	a.SetContainer("c1", "o", slots(920), 1000)
	if a.List()[0].Group != "Main" {
		t.Fatalf("loc-key tab should become 'Main', got %q", a.List()[0].Group)
	}
}

func TestBankTabsGrouping(t *testing.T) {
	a, book := newAgg(t)
	book.SetEMV(920, 0, 1000, 1000)
	// Two named tabs; only "Items" opened.
	a.SetBankVault([]string{"o1", "o2"}, []string{"Items", "Resources"})
	a.SetContainer("c1", "o1", slots(920, 837), 1000)

	s := a.Summary(1000)
	// One bank city group (no current city yet → "Bank").
	var bank *model.CitySummary
	for i := range s.Cities {
		if !s.Cities[i].IsInventory {
			bank = &s.Cities[i]
		}
	}
	if bank == nil {
		t.Fatal("no bank city group")
	}
	tabs := map[string]model.TabSummary{}
	for _, tb := range bank.Tabs {
		tabs[tb.Name] = tb
	}
	items, okI := tabs["Items"]
	res, okR := tabs["Resources"]
	if !okI || !okR {
		t.Fatalf("want both tabs, got %v", bank.Tabs)
	}
	if !items.Opened || items.ItemCount != 2 || items.Subtotal != 1000 {
		t.Fatalf("Items tab wrong: %+v", items)
	}
	if res.Opened { // named via BankVaultInfo but never opened
		t.Fatalf("Resources must be not-opened, got %+v", res)
	}
}

func TestTabReObserveReplaces(t *testing.T) {
	a, _ := newAgg(t)
	a.SetBankVault([]string{"o1"}, []string{"Items"})
	a.SetContainer("c1", "o1", slots(920, 837), 1000)
	a.SetContainer("c1", "o1", slots(920), 1100) // item moved out → REPLACE
	s := a.Summary(1100)
	for _, c := range s.Cities {
		for _, tb := range c.Tabs {
			if tb.Name == "Items" && tb.ItemCount != 1 {
				t.Fatalf("tab re-observe should replace → 1 item, got %d", tb.ItemCount)
			}
		}
	}
}

func TestCurrentCityGroupsBank(t *testing.T) {
	a, book := newAgg(t)
	book.SetEMV(920, 0, 500, 1000)
	a.SetBankVault([]string{"o1"}, []string{"Items"})

	// In Caerleon → bank container tagged Caerleon.
	a.SetCurrentCity("Caerleon")
	a.SetContainer("c1", "o1", slots(920), 1000)
	// Inventory is city-independent.
	a.SetContainer("inv", "player", slots(920), 1000)

	s := a.Summary(1000)
	var caer, inv *model.CitySummary
	for i := range s.Cities {
		switch {
		case s.Cities[i].Name == "Caerleon":
			caer = &s.Cities[i]
		case s.Cities[i].IsInventory:
			inv = &s.Cities[i]
		}
	}
	if caer == nil || caer.Total != 500 {
		t.Fatalf("Caerleon bank group missing/total wrong: %+v", caer)
	}
	if inv == nil {
		t.Fatal("inventory group must exist independent of city")
	}
	// A bank row carries the city; an inventory row does not.
	for _, r := range a.List() {
		if r.Location == model.LocBank && r.City != "Caerleon" {
			t.Fatalf("bank row city = %q, want Caerleon", r.City)
		}
		if r.Location == model.LocInventory && r.City != "" {
			t.Fatalf("inventory row city = %q, want empty", r.City)
		}
	}
}

// Review fix (008): the player's own containers are pinned — a long session that
// churns >containerCap loot/bank containers must never evict self-bag, or every
// subsequent bag put would silently no-op (frozen bag until relog).
func TestPinnedSelfContainerSurvivesEviction(t *testing.T) {
	a, _ := newAgg(t)
	a.EnsureSelfContainer("self-bag", "Bag") // pinned, first inserted (startup order)
	a.PutItem("self-bag", 900001, ItemRef{Index: 920}, 100)
	for i := 0; i < containerCap+50; i++ {
		a.SetContainer(fmt.Sprintf("churn-%d", i), "", nil, int64(200+i))
	}
	if !a.PutItem("self-bag", 900002, ItemRef{Index: 920}, 9999) {
		t.Fatal("self-bag was evicted by container churn (must be pinned)")
	}
}

// Review fix (008): pre-created-but-never-observed containers must not surface in
// the Summary — an empty fresh-looking Bag/Equipped before any capture fakes data.
func TestUnseenPrecreatedContainersHidden(t *testing.T) {
	a, _ := newAgg(t)
	a.EnsureSelfContainer("self-bag", "Bag")
	a.EnsureSelfContainer("self-equipped", "Equipped")
	if got := len(a.Summary(1000).Cities); got != 0 {
		t.Fatalf("unseen pre-created containers must be hidden, got %d cities", got)
	}
	// First real data makes them visible.
	a.PutItem("self-bag", 900003, ItemRef{Index: 920}, 2000)
	if got := len(a.Summary(3000).Cities); got == 0 {
		t.Fatal("container with real data must surface")
	}
}

func TestIncrementalMoveAndDelete(t *testing.T) {
	a, _ := newAgg(t)
	a.SetBankVault([]string{"o1"}, []string{"Items"})
	// Bank tab holds object 100 (item 920); the bag is a KNOWN self container
	// (pre-created by the GUID bridge — 008: puts into unknown containers are no-ops).
	a.SetSelfContainer("bagGuid", "Bag", nil, 900)
	a.SetContainer("bankGuid", "o1", []SlotItem{{ObjID: 100, Ref: ItemRef{Index: 920}}}, 1000)
	if n := len(a.List()); n != 1 {
		t.Fatalf("after snapshot want 1, got %d", n)
	}
	// Move it to the bag → still exactly one item, now inventory.
	a.PutItem("bagGuid", 100, ItemRef{Index: 920}, 1100)
	// Put into an UNKNOWN container is a no-op (008): nothing moves, nothing created.
	a.PutItem("mysteryGuid", 100, ItemRef{Index: 920}, 1150)
	rows := a.List()
	if len(rows) != 1 {
		t.Fatalf("after move want 1 (no dup), got %d", len(rows))
	}
	if rows[0].Location != model.LocInventory {
		t.Fatalf("moved item should be in inventory, got %s", rows[0].Location)
	}
	// Delete it → gone.
	a.DeleteItem(100, 1200)
	if n := len(a.List()); n != 0 {
		t.Fatalf("after delete want 0, got %d", n)
	}
}

func TestSummaryTotals(t *testing.T) {
	a, book := newAgg(t)
	book.SetEMV(920, 0, 3360, 1000)
	a.SetContainer("c1", "owner", slots(920, 837), 1000)
	s := a.Summary(1000)
	if s.TotalValue != 3360 || s.UnvaluedCount != 1 {
		t.Fatalf("summary = %+v, want total 3360 unvalued 1", s)
	}
	// Only inventory was observed → exactly one city group, the inventory group.
	if len(s.Cities) != 1 || !s.Cities[0].IsInventory {
		t.Fatalf("want single inventory city group, got %+v", s.Cities)
	}
}

// 010: K bank-overview summary tabs — REPLACE semantics, real tab names, synthetic
// negative row ids that can never collide with real object ids or enter move paths.
func TestVaultSummaryTab(t *testing.T) {
	a, _ := newAgg(t)
	rows := []ItemRef{{Index: 920, Count: 19}, {Index: 837, Count: 2}}
	a.SetVaultSummaryTab("tabg1", "Thetford", "Hammadde", rows, 1000)
	list := a.List()
	if len(list) != 2 {
		t.Fatalf("rows = %d, want 2", len(list))
	}
	for _, r := range list {
		if r.Location != model.LocBank || r.City != "Thetford" || r.Group != "Hammadde" {
			t.Fatalf("row misplaced: %+v", r)
		}
		if r.ObjID >= 0 {
			t.Fatalf("summary row must use synthetic NEGATIVE id, got %d", r.ObjID)
		}
	}
	// REPLACE: second snapshot with one row → one row.
	a.SetVaultSummaryTab("tabg1", "Thetford", "Hammadde", rows[:1], 2000)
	if n := len(a.List()); n != 1 {
		t.Fatalf("after replace want 1, got %d", n)
	}
	// Synthetic ids are invisible to the incremental move/delete paths.
	synth := a.List()[0].ObjID
	a.DeleteItem(synth, 3000) // defensive: even a direct delete of a negative id must not corrupt
	if !a.PutItem("tabg1", 999, ItemRef{Index: 920}, 3500) {
		t.Fatal("known summary tab must accept a real put (same container semantics)")
	}
}

// 010: same tab guid seen via K summary AND physical open (99) → ONE container,
// last writer wins (contract rule 5).
func TestVaultSummaryMergesWithPhysicalOpen(t *testing.T) {
	a, _ := newAgg(t)
	a.SetBankVault([]string{"owner1"}, []string{"Eski Ad"})
	a.SetContainer("tabg2", "owner1", []SlotItem{{ObjID: 500, Ref: ItemRef{Index: 920}}}, 1000)
	// K summary arrives later for the SAME guid: content + real name replace.
	a.SetVaultSummaryTab("tabg2", "Thetford", "Hammadde", []ItemRef{{Index: 837, Count: 3}}, 2000)
	list := a.List()
	if len(list) != 1 || list[0].Item.Index != 837 || list[0].Group != "Hammadde" {
		t.Fatalf("K summary must replace content+name: %+v", list)
	}
	// Physical open later wins back with object rows.
	a.SetContainer("tabg2", "owner1", []SlotItem{{ObjID: 501, Ref: ItemRef{Index: 920}}}, 3000)
	list = a.List()
	if len(list) != 1 || list[0].ObjID != 501 {
		t.Fatalf("physical open must win back: %+v", list)
	}
}

// 010: per-city vault totals surface on the Summary and REPLACE wholesale.
func TestCityVaultValues(t *testing.T) {
	a, _ := newAgg(t)
	a.SetCityVaultValues(map[string]int64{"Thetford": 11094210, "Lymhurst": 318725}, 1000)
	sum := a.Summary(2000)
	var got int64
	for _, c := range sum.Cities {
		if c.Name == "Thetford" {
			got = c.VaultValue
		}
	}
	if got != 11094210 {
		t.Fatalf("Thetford VaultValue = %d, want 11094210", got)
	}
	// Second K opening reports fewer locations → old entries drop (REPLACE).
	a.SetCityVaultValues(map[string]int64{"Thetford": 12000000}, 3000)
	sum = a.Summary(4000)
	for _, c := range sum.Cities {
		if c.Name == "Lymhurst" && c.VaultValue != 0 {
			t.Fatalf("stale Lymhurst value survived replace: %d", c.VaultValue)
		}
		if c.Name == "Thetford" && c.VaultValue != 12000000 {
			t.Fatalf("Thetford not updated: %d", c.VaultValue)
		}
	}
}

// 010 review: reverse-direction dedup — a physical open AFTER a K summary evicts
// the overlapping summary container (no double count, no immortal stale summary),
// and a late city backfill re-runs the eviction.
func TestPhysicalOpenEvictsSummary(t *testing.T) {
	a, _ := newAgg(t)
	a.SetCurrentCity("Martlock")
	a.SetBankVault([]string{"ownerX"}, []string{"Main"})
	a.SetVaultSummaryTab("vault:Martlock:Main", "Martlock", "Main", []ItemRef{{Index: 920, Count: 5}}, 1000)
	// Physical open under a DIFFERENT guid, same (city, tab).
	a.SetContainer("physGuid", "ownerX", []SlotItem{{ObjID: 900101, Ref: ItemRef{Index: 837}}}, 2000)
	list := a.List()
	if len(list) != 1 || list[0].ObjID != 900101 {
		t.Fatalf("summary must be evicted by the physical open: %+v", list)
	}

	// City-less physical open first, summary meanwhile, then city learned late:
	// the backfill must also kill the overlap.
	b, _ := newAgg(t)
	b.SetBankVault([]string{"ownerY"}, []string{"Res"}) // currentCity "" → city-less
	b.SetContainer("physG2", "ownerY", []SlotItem{{ObjID: 900201, Ref: ItemRef{Index: 920}}}, 1000)
	b.SetVaultSummaryTab("vault:Lymhurst:Res", "Lymhurst", "Res", []ItemRef{{Index: 837, Count: 2}}, 1500)
	b.SetCurrentCity("Lymhurst")
	list = b.List()
	if len(list) != 1 || list[0].ObjID != 900201 || list[0].City != "Lymhurst" {
		t.Fatalf("backfill must migrate + evict the summary overlap: %+v", list)
	}
}

// 020 US1 / C2 — Snapshot()→SeedContainers() hydrates a fresh aggregator with the persisted
// items at their original lastSeen (stale), and a live SetContainer(sameID) REPLACES only
// that container while other (stale) containers are untouched (SC-002).
func TestSeedContainersHydratesAndReplaces(t *testing.T) {
	src, _ := newAgg(t)
	src.SetContainer("c1", "owner", slots(920, 837), 1000) // 2 items, lastSeen 1000
	snaps := src.Snapshot()
	if len(snaps) != 1 || len(snaps[0].Items) != 2 {
		t.Fatalf("snapshot must carry the container's 2 items, got %+v", snaps)
	}

	dst, _ := newAgg(t)
	dst.SeedContainers(snaps)
	rows := dst.List()
	if len(rows) != 2 {
		t.Fatalf("hydrate must restore 2 items, got %d", len(rows))
	}
	for _, r := range rows {
		if r.LastSeen != 1000 {
			t.Fatalf("hydrated item must carry the persisted lastSeen 1000 (stale), got %d", r.LastSeen)
		}
	}

	// A second stale container arrives from persistence.
	dst.SeedContainers([]ContainerSnapshot{{
		GUID: "c2", Location: model.LocInventory, Tab: "Bag", LastSeen: 500,
		Items: []model.HoldingItem{{ObjID: 99, Item: model.Item{Index: 837}, Count: 1, LastSeen: 500}},
	}})
	// Live re-observe of c1 replaces it (now 1 item); c2 stays (SC-002 no cross-effect).
	dst.SetContainer("c1", "owner", slots(920), 2000)
	if n := len(dst.List()); n != 2 { // c1:1 + c2:1
		t.Fatalf("after replacing c1, want 2 rows (c1:1 + c2:1), got %d", n)
	}
}

// 020 US1 — SeedContainers never clobbers a container that live data already claimed.
func TestSeedContainersDoesNotClobberLive(t *testing.T) {
	a, _ := newAgg(t)
	a.SetContainer("c1", "owner", slots(920), 2000) // live: 1 item
	a.SeedContainers([]ContainerSnapshot{{
		GUID: "c1", Location: model.LocInventory, Tab: "Bag", LastSeen: 100,
		Items: []model.HoldingItem{{ObjID: 5, Item: model.Item{Index: 837}, Count: 9, LastSeen: 100}},
	}})
	rows := a.List()
	if len(rows) != 1 || rows[0].Item.Index != 920 {
		t.Fatalf("stale seed must not overwrite live c1, got %+v", rows)
	}
}

// 020 fix — a self container pre-created empty by EnsureSelfContainer (unobserved) IS
// hydrated from persistence, then replaced when live own-state arrives.
func TestSeedContainersFillsPreCreatedSelfContainer(t *testing.T) {
	a, _ := newAgg(t)
	a.EnsureSelfContainer("self-bag", "Bag") // pre-created empty, lastSeen 0
	a.SeedContainers([]ContainerSnapshot{{
		GUID: "self-bag", Location: model.LocInventory, Tab: "Bag", LastSeen: 100, Pinned: true,
		Items: []model.HoldingItem{{ObjID: 7, Item: model.Item{Index: 920}, Count: 1, LastSeen: 100}},
	}})
	if rows := a.List(); len(rows) != 1 || rows[0].Item.Index != 920 {
		t.Fatalf("persisted self-bag must hydrate into the pre-created container, got %+v", rows)
	}
	// Live own-state then replaces it.
	a.SetSelfContainer("self-bag", "Bag", []SlotItem{{ObjID: 8, Ref: ItemRef{Index: 837}}}, 2000)
	if r := a.List(); len(r) != 1 || r[0].Item.Index != 837 {
		t.Fatalf("live own-state must replace the hydrated self-bag, got %+v", r)
	}
}

// 010/020 live-test — a K summary and a physical container for the SAME (city,tab) must be
// counted ONCE (physical wins), even when the write-time reconciliation missed and left both
// in the map. Read-time dedup guards Summary + List (no double-counted net worth).
func TestSummaryDedupsPhysicalOverSummary(t *testing.T) {
	a, book := newAgg(t)
	book.SetEMV(920, 0, 1000, 1000) // value the item
	// The exact double-count state: a summary + a physical for Lymhurst/Tab1, both holding 3.
	a.containers["sum"] = &container{location: model.LocBank, city: "Lymhurst", tab: "Tab1", summary: true, lastSeen: 1000,
		items: map[int]model.HoldingItem{-1: {ObjID: -1, Item: model.Item{Index: 920}, Count: 3}}}
	a.containers["phys"] = &container{location: model.LocBank, city: "Lymhurst", tab: "Tab1", summary: false, lastSeen: 1000,
		items: map[int]model.HoldingItem{5: {ObjID: 5, Item: model.Item{Index: 920}, Count: 3}}}
	a.order = []string{"sum", "phys"}

	if s := a.Summary(2000); s.TotalValue != 3000 { // 3×1000 once, NOT 6000
		t.Fatalf("physical must win, summary skipped: want TotalValue 3000, got %d", s.TotalValue)
	}
	if n := len(a.List()); n != 1 { // the physical stack once, not 2 (physical + summary)
		t.Fatalf("List must dedup the summary row: want 1, got %d", n)
	}
}

// 020 live-test (2026-07-10) — app started already standing at a bank, so event 163 never
// fired and currentCity stayed "". A physical bank open must still land under the real city
// (not a "Bank" ghost) and dedup against its K summary: the city is INFERRED from the K
// overview by matching a tab name unique to one city, then pinned so shared names inherit it.
func TestBankCityInferredFromSummaryMidSession(t *testing.T) {
	a, _ := newAgg(t)
	// K overview arrives for TWO cities. "Setler" is unique to Fort Sterling; "Hammadde"
	// exists in both → ambiguous on its own.
	a.SetVaultSummaryTab("vault:Fort Sterling:Setler", "Fort Sterling", "Setler", []ItemRef{{Index: 920, Count: 4}}, 1000)
	a.SetVaultSummaryTab("vault:Fort Sterling:Hammadde", "Fort Sterling", "Hammadde", []ItemRef{{Index: 837, Count: 5}}, 1000)
	a.SetVaultSummaryTab("vault:Thetford:Hammadde", "Thetford", "Hammadde", []ItemRef{{Index: 837, Count: 9}}, 1000)

	// Physical open at Fort Sterling (currentCity still ""). Ambiguous "Hammadde" opens FIRST —
	// it cannot resolve alone, but the unique "Setler" pins the city; once pinned the earlier
	// city-less Hammadde tab is migrated to Fort Sterling by the heal.
	a.SetBankVault([]string{"oHam", "oSet"}, []string{"Hammadde", "Setler"})
	a.SetContainer("physHam", "oHam", []SlotItem{{ObjID: 5001, Ref: ItemRef{Index: 837}}}, 2000)
	a.SetContainer("physSet", "oSet", []SlotItem{{ObjID: 5002, Ref: ItemRef{Index: 920}}}, 2100)

	for _, r := range a.List() {
		if r.Location == model.LocBank && r.City == "" {
			t.Fatalf("physical bank tab left city-less (Bank ghost): %+v", r)
		}
	}
	// Both physical tabs must be Fort Sterling and each counted ONCE (their FS summaries evicted;
	// the Thetford Hammadde summary is a different city and survives).
	rows := a.List()
	fsPhysical := 0
	for _, r := range rows {
		if r.City == "Fort Sterling" && r.ObjID >= 0 {
			fsPhysical++
		}
	}
	if fsPhysical != 2 {
		t.Fatalf("both FS physical tabs must land under Fort Sterling, got %d in %+v", fsPhysical, rows)
	}
	// No Fort Sterling summary may survive alongside its physical peer (dedup).
	for _, r := range rows {
		if r.City == "Fort Sterling" && r.ObjID < 0 {
			t.Fatalf("FS summary must be deduped by its physical peer: %+v", r)
		}
	}
	// Thetford Hammadde (no physical open) still shows as a summary.
	thetford := false
	for _, r := range rows {
		if r.City == "Thetford" && r.Group == "Hammadde" {
			thetford = true
		}
	}
	if !thetford {
		t.Fatalf("un-opened Thetford summary must remain: %+v", rows)
	}
}
