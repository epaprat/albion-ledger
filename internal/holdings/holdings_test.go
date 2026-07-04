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
