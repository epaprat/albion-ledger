package wailsadapter

import (
	"context"
	"testing"

	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/holdings"
	"github.com/epaprat/albion-ledger/internal/port"
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
	s.IngestContainer("c1", "owner", []holdings.SlotItem{{ObjID: 1, Ref: holdings.ItemRef{Index: 920}}})

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
	s.IngestContainer("inv", "playerOwner", []holdings.SlotItem{{ObjID: 1, Ref: holdings.ItemRef{Index: 920}}, {ObjID: 2, Ref: holdings.ItemRef{Index: 837}}})
	// Bank tab "Items": one valued (920).
	s.IngestBankVault([]string{"bankOwner"}, []string{"Items"})
	s.IngestContainer("bank1", "bankOwner", []holdings.SlotItem{{ObjID: 3, Ref: holdings.ItemRef{Index: 920}}})

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

// 010: the external (AODP) layer prices held-but-unvalued items, only queries the
// missing ones, and every in-game source outranks it.
type fakeFetcher struct {
	prices []port.ExternalPrice
	asked  []string
}

func (f *fakeFetcher) Fetch(_ context.Context, names []string) ([]port.ExternalPrice, error) {
	f.asked = names
	return f.prices, nil
}

func TestExternalPricesFillGapsButYieldToInGame(t *testing.T) {
	s, _, book := newHoldSvc(t)
	s.IngestVaultSummaryTab("vault:X:Tab", "X", "Tab", []holdings.ItemRef{{Index: 920, Count: 2, Quality: 1}})

	f := &fakeFetcher{prices: []port.ExternalPrice{{UniqueName: "T7_WOOD", Quality: 1, Silver: 55}}}
	if n := s.RefreshExternalPrices(context.Background(), f); n != 1 {
		t.Fatalf("fetched = %d, want 1", n)
	}
	if len(f.asked) != 1 || f.asked[0] != "T7_WOOD" {
		t.Fatalf("must query exactly the missing name, got %v", f.asked)
	}
	rows := s.ListHoldings()
	if len(rows) != 1 || rows[0].Valuation.Amount != 55 || rows[0].Valuation.Source != model.SourceExternal {
		t.Fatalf("external price must fill the gap: %+v", rows[0].Valuation)
	}

	// In-game EMV arrives → outranks the external base layer.
	book.SetEMV(920, 1, 70, 900)
	rows = s.ListHoldings()
	if rows[0].Valuation.Amount != 70 || rows[0].Valuation.Source != model.SourceServerEstimate {
		t.Fatalf("in-game EMV must override external: %+v", rows[0].Valuation)
	}

	// Nothing missing anymore → no query at all.
	f.asked = nil
	if n := s.RefreshExternalPrices(context.Background(), f); n != 0 || f.asked != nil {
		t.Fatalf("no-missing must skip the fetch: n=%d asked=%v", n, f.asked)
	}
}
