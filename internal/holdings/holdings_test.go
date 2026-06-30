package holdings

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

const cat = `{"items":[
  {"index":920,"uniqueName":"T7_WOOD","name":"Ashenbark Logs"},
  {"index":837,"uniqueName":"T8_MEAL","name":"Avalonian Stew"}
]}`

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
	a.SetContainer("c1", "owner", []ItemRef{{920, 0}, {837, 0}}, 1000)
	if len(a.List()) != 2 {
		t.Fatalf("want 2 items, got %d", len(a.List()))
	}
	a.SetContainer("c1", "owner", []ItemRef{{920, 0}}, 1100) // item moved out → REPLACE
	if n := len(a.List()); n != 1 {
		t.Fatalf("after replace want 1, got %d (duplicate/append bug)", n)
	}
}

func TestInventoryVsBankByOwner(t *testing.T) {
	a, _ := newAgg(t)
	// Unknown owner → Inventory.
	a.SetContainer("c1", "playerOwner", []ItemRef{{920, 0}}, 1000)
	if got := a.List()[0]; got.Location != model.LocInventory || got.Group != "Inventory" {
		t.Fatalf("unmatched owner should be Inventory, got loc=%s group=%q", got.Location, got.Group)
	}
	// Owner declared as a bank tab → location bank, tab name as group.
	a.SetBankVault([]string{"bankOwner"}, []string{"Items"})
	a.SetContainer("c2", "bankOwner", []ItemRef{{837, 0}}, 1100)
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
	a.SetContainer("c1", "o", []ItemRef{{920, 0}}, 1000)
	if a.List()[0].Group != "Main" {
		t.Fatalf("loc-key tab should become 'Main', got %q", a.List()[0].Group)
	}
}

func TestSummaryTotals(t *testing.T) {
	a, book := newAgg(t)
	book.SetEMV(920, 0, 3360, 1000)
	a.SetContainer("c1", "owner", []ItemRef{{920, 0}, {837, 0}}, 1000)
	s := a.Summary(1000)
	if s.TotalValue != 3360 || s.UnvaluedCount != 1 {
		t.Fatalf("summary = %+v, want total 3360 unvalued 1", s)
	}
	// Only inventory was observed → exactly one city group, the inventory group.
	if len(s.Cities) != 1 || !s.Cities[0].IsInventory {
		t.Fatalf("want single inventory city group, got %+v", s.Cities)
	}
}
