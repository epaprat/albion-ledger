package wailsadapter

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

func countEvent(em *fakeEmitter, name string) int {
	n := 0
	for _, e := range em.events {
		if e == name {
			n++
		}
	}
	return n
}

func TestAddTrade_DedupAndEmitOnlyOnChange(t *testing.T) {
	s, em, _ := newHoldSvc(t)
	tr := model.Trade{TradeID: "mail:1001", Direction: model.TradeSold, Source: model.TradeSourceMail,
		ItemID: "T7_WOOD", PartialAmount: 5, TotalAmount: 5, Gross: 50_000, SalesTax: 2_000, Net: 48_000, Received: 100}

	s.AddTrade(tr)
	s.AddTrade(tr) // identical re-read → no change, no extra emit

	if got := s.Trades(); len(got) != 1 {
		t.Fatalf("expected 1 trade after dedup, got %d", len(got))
	}
	if n := countEvent(em, EventTradesChanged); n != 1 {
		t.Fatalf("expected 1 trades emit (only on change), got %d", n)
	}
	// Item name resolved from the catalog (T7_WOOD → Ashenbark Logs).
	if s.Trades()[0].ItemName != "Ashenbark Logs" {
		t.Fatalf("item name not resolved: %q", s.Trades()[0].ItemName)
	}
}

func TestTradeSummary_Breakdown(t *testing.T) {
	s, _, _ := newHoldSvc(t)
	// Two sales (gross 300+200, tax 12+8, net 288+192) and one buy (gross 150, net -150).
	s.AddTrade(model.Trade{TradeID: "a", Direction: model.TradeSold, Gross: 300, SalesTax: 12, Net: 288})
	s.AddTrade(model.Trade{TradeID: "b", Direction: model.TradeSold, Gross: 200, SalesTax: 8, Net: 192})
	s.AddTrade(model.Trade{TradeID: "c", Direction: model.TradeBought, Gross: 150, Net: -150})
	s.AddTrade(model.Trade{TradeID: "d", Direction: model.TradeSold, Gross: 100, SetupFee: 5, SalesTax: 4, Net: 96})

	sum := s.TradeSummary()
	if sum.GrossIncome != 600 || sum.GrossExpense != 150 {
		t.Fatalf("gross wrong: %+v", sum)
	}
	if sum.SalesTax != 24 || sum.SetupFee != 5 {
		t.Fatalf("tax/setup wrong: %+v", sum)
	}
	// Net = 288+192+96 − 150 = 426.
	if sum.Net != 426 || sum.Count != 4 {
		t.Fatalf("net/count wrong: %+v", sum)
	}
}

func TestSeedTrades_RestoresLedger(t *testing.T) {
	s, _, _ := newHoldSvc(t)
	s.SeedTrades([]model.Trade{
		{TradeID: "mail:1", Direction: model.TradeSold, Gross: 500, Net: 480, Received: 10},
		{TradeID: "mail:2", Direction: model.TradeBought, Gross: 100, Net: -100, Received: 20},
	})
	if got := s.Trades(); len(got) != 2 || got[0].TradeID != "mail:2" { // newest (received 20) first
		t.Fatalf("seed restore wrong: %+v", got)
	}
	if sum := s.TradeSummary(); sum.Net != 380 {
		t.Fatalf("seeded net wrong: %+v", sum)
	}
}
