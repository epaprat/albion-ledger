package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

func TestTradesRoundTripAndDedup(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "trades.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	sell := model.Trade{TradeID: "mail:1001", Direction: model.TradeSold, Source: model.TradeSourceMail,
		ItemID: "T5_2H_SHAPESHIFTER_SET3@1", PartialAmount: 1, TotalAmount: 1,
		Gross: 154_984, SalesTax: 6_199, Net: 148_785, TaxEstimated: true, UnitSilver: 154_984, Received: 1_549_840_000, LocationID: "3005"}
	buy := model.Trade{TradeID: "mail:1002", Direction: model.TradeBought, Source: model.TradeSourceMail,
		ItemID: "T7_ALCHEMY_RARE_ENT", PartialAmount: 10, TotalAmount: 10,
		Gross: 1_100_010, Net: -1_100_010, UnitSilver: 110_001, Received: 1_549_850_000}

	if err := db.SaveTrade(ctx, sell); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveTrade(ctx, buy); err != nil {
		t.Fatal(err)
	}
	// Same id again → upsert, not a second row (FR-010).
	if err := db.SaveTrade(ctx, sell); err != nil {
		t.Fatal(err)
	}

	got, err := db.LoadTrades(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 trades after dedup, got %d", len(got))
	}
	// Newest first (buy has the later received).
	if got[0].TradeID != "mail:1002" || got[1].TradeID != "mail:1001" {
		t.Fatalf("order wrong: %s, %s", got[0].TradeID, got[1].TradeID)
	}
	if got[1] != sell {
		t.Fatalf("round-trip mismatch\n got=%+v\nwant=%+v", got[1], sell)
	}
}

func TestTradesCapPrunesOldest(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "cap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for i := 0; i < tradesCap+50; i++ {
		if err := db.SaveTrade(ctx, model.Trade{TradeID: itoa(i + 1), Direction: model.TradeSold, Received: int64(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.LoadTrades(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != tradesCap {
		t.Fatalf("expected cap %d, got %d", tradesCap, len(got))
	}
	// The 50 oldest (received 1..50) were pruned; newest kept.
	if got[len(got)-1].Received != 51 {
		t.Fatalf("oldest kept should be received 51, got %d", got[len(got)-1].Received)
	}
}

// itoa keeps trade ids stable-sortable as zero-padded strings for the cap test.
func itoa(n int) string {
	const width = 8
	b := []byte("00000000")
	for i := width - 1; i >= 0 && n > 0; i-- {
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return "t" + string(b)
}
