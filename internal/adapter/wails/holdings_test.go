package wailsadapter

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/holdings"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

const holdCat = `{"items":[{"index":920,"uniqueName":"T7_WOOD","name":"Ashenbark Logs"}]}`

func newHoldSvc(t *testing.T) (*Service, *fakeEmitter, *valuation.Book) {
	t.Helper()
	c, err := catalog.New([]byte(holdCat))
	if err != nil {
		t.Fatal(err)
	}
	book := valuation.NewBook()
	em := &fakeEmitter{}
	return NewService(c, book, valuation.NewValuer(book, model.DefaultStaleAfterMS), em, 100, func() int64 { return 1000 }), em, book
}

func TestIngestContainerResolvedValued(t *testing.T) {
	s, em, book := newHoldSvc(t)
	book.SetEMV(920, 0, 3360, 1000)
	s.IngestContainer("c1", "owner", []holdings.ItemRef{{Index: 920, Quality: 0}})

	h := s.ListHoldings()
	if len(h) != 1 || h[0].Item.DisplayName != "Ashenbark Logs" {
		t.Fatalf("holdings = %+v", h)
	}
	if h[0].Valuation.Amount != 3360 || h[0].Location != model.LocInventory {
		t.Fatalf("row = %+v", h[0])
	}
	if s.HoldingsSummary().TotalValue != 3360 {
		t.Fatalf("total = %d", s.HoldingsSummary().TotalValue)
	}
	sawHoldings := false
	for _, e := range em.events {
		sawHoldings = sawHoldings || e == EventHoldingsChanged
	}
	if !sawHoldings {
		t.Fatalf("expected holdings:changed, got %v", em.events)
	}
}

func TestHoldingsSummaryNestedTotals(t *testing.T) {
	s, em, book := newHoldSvc(t)
	book.SetEMV(920, 0, 3360, 1000)
	// Inventory: one valued (920) + one unvalued (837, not in catalog book).
	s.IngestContainerSlots("inv", "playerOwner", []int{920, 837})
	// Bank tab "Items": one valued (920).
	s.IngestBankVault([]string{"bankOwner"}, []string{"Items"})
	s.IngestContainerSlots("bank1", "bankOwner", []int{920})

	sum := s.HoldingsSummary()
	if sum.TotalValue != 6720 { // 3360 (inv) + 3360 (bank)
		t.Fatalf("grand total = %d, want 6720", sum.TotalValue)
	}
	if sum.UnvaluedCount != 1 {
		t.Fatalf("unvalued = %d, want 1", sum.UnvaluedCount)
	}
	// Inventory group first; a separate bank city group exists.
	if len(sum.Cities) < 2 || !sum.Cities[0].IsInventory {
		t.Fatalf("want inventory-first + a bank group, got %+v", sum.Cities)
	}
	var bank *model.CitySummary
	for i := range sum.Cities {
		if !sum.Cities[i].IsInventory {
			bank = &sum.Cities[i]
		}
	}
	if bank == nil || len(bank.Tabs) == 0 || bank.Tabs[0].Name != "Items" || bank.Total != 3360 {
		t.Fatalf("bank group/tab wrong: %+v", bank)
	}
	saw := false
	for _, e := range em.events {
		saw = saw || e == EventHoldingsChanged
	}
	if !saw {
		t.Fatalf("expected holdings:changed, got %v", em.events)
	}
}

func TestEquipmentAndSpec(t *testing.T) {
	s, _, _ := newHoldSvc(t)
	s.IngestEquipment([]holdings.ItemRef{{Index: 920, Quality: 2}})
	if got := s.ListHoldings(); len(got) != 1 || got[0].Location != model.LocEquipped {
		t.Fatalf("equipped = %+v", got)
	}
	s.SetSpec([]int{8, 0, 86})
	if sp := s.Spec(); len(sp.Masteries) != 3 || sp.Masteries[2].Level != 86 {
		t.Fatalf("spec = %+v", sp)
	}
}
