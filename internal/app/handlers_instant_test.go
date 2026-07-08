package app

// Instant-trade wallet-correlation tests (017 expansion): drive the real dispatch path —
// an instant sell/buy/quicksell REQUEST arms a context, and the following E:81 wallet
// delta becomes that trade's silver. A wallet change with no armed trade is ignored.

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

// walletEvent builds an E:81 wallet balance packet (k1 = silver ×10000).
func walletEvent(silver int64) map[byte]interface{} {
	return map[byte]interface{}{1: silver * 10000}
}

func TestInstant_SellFromWalletDelta(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindEvent, 81, walletEvent(1000)) // baseline
	// Instant sell: item type 3543 (Adept's Guardian Helmet), amount 1.
	p.dispatch(probe.KindRequest, 315, map[byte]interface{}{1: int64(999), 2: int32(3543), 4: int32(1)})
	p.dispatch(probe.KindEvent, 81, walletEvent(1561)) // +561 net proceeds

	trades := svc.Trades()
	if len(trades) != 1 {
		t.Fatalf("expected 1 instant trade, got %d", len(trades))
	}
	tr := trades[0]
	// net = wallet delta (561, exact); gross reconstructed from 4% tax = round(561/0.96)=584.
	if tr.Direction != "sold" || tr.Source != "instant" || tr.Net != 561 || tr.ItemIndex != 3543 {
		t.Fatalf("instant sell wrong: %+v", tr)
	}
	if tr.Gross != 584 || tr.SalesTax != 23 || !tr.TaxEstimated {
		t.Fatalf("breakdown wrong: %+v", tr)
	}
}

func TestInstant_BuyFromWalletDelta(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindEvent, 81, walletEvent(100000)) // baseline
	p.dispatch(probe.KindRequest, 83, map[byte]interface{}{1: int32(5), 2: int64(3176516525)})
	p.dispatch(probe.KindEvent, 81, walletEvent(21555)) // −78445 spent

	trades := svc.Trades()
	if len(trades) != 1 {
		t.Fatalf("expected 1 instant buy, got %d", len(trades))
	}
	tr := trades[0]
	if tr.Direction != "bought" || tr.Net != -78_445 || tr.Gross != 78_445 || tr.SalesTax != 0 {
		t.Fatalf("instant buy wrong: %+v", tr)
	}
}

func TestInstant_BuyItemFromOfferCache(t *testing.T) {
	svc, p := newGlue(t)
	// Browsing offers (R:81) caches order id → item; the instant buy then names it.
	offer := `{"Id":999,"UnitPriceSilver":100000,"ItemTypeId":"T7_WOOD","EnchantmentLevel":0,"QualityLevel":1,"AuctionType":"offer"}`
	p.dispatch(probe.KindResponse, 81, map[byte]interface{}{0: []string{offer}})
	p.dispatch(probe.KindEvent, 81, walletEvent(100000)) // baseline
	p.dispatch(probe.KindRequest, 83, map[byte]interface{}{1: int32(3), 2: int64(999)})
	p.dispatch(probe.KindEvent, 81, walletEvent(70000)) // −30000 spent

	trades := svc.Trades()
	if len(trades) != 1 || trades[0].ItemName != "Ashenbark Logs" {
		t.Fatalf("instant buy item must resolve from offer cache, got %+v", trades)
	}
	if trades[0].Net != -30_000 {
		t.Fatalf("instant buy net wrong: %d", trades[0].Net)
	}
}

func TestInstant_QuicksellAggregatesBurst(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindEvent, 81, walletEvent(1000)) // baseline
	p.dispatch(probe.KindRequest, 485, map[byte]interface{}{1: make([]int32, 3), 2: make([]int64, 3)})
	// Three items sell in a burst → three wallet increases, one aggregate trade.
	p.dispatch(probe.KindEvent, 81, walletEvent(1100)) // +100
	p.dispatch(probe.KindEvent, 81, walletEvent(1250)) // +150
	p.dispatch(probe.KindEvent, 81, walletEvent(1300)) // +50

	trades := svc.Trades()
	if len(trades) != 1 {
		t.Fatalf("expected 1 aggregate quicksell trade, got %d", len(trades))
	}
	tr := trades[0]
	if tr.Source != "quicksell" || tr.Net != 300 || tr.TotalAmount != 3 {
		t.Fatalf("quicksell aggregate wrong: %+v", tr)
	}
	if svc.Trades()[0].ItemName != "Quicksell (3 items)" {
		t.Fatalf("quicksell label wrong: %q", svc.Trades()[0].ItemName)
	}
}

func TestInstant_BuyDisarmsAfterOneDelta(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindEvent, 81, walletEvent(100000)) // baseline
	p.dispatch(probe.KindRequest, 83, map[byte]interface{}{1: int32(10), 2: int64(999)})
	p.dispatch(probe.KindEvent, 81, walletEvent(90000)) // −10000 the buy
	p.dispatch(probe.KindEvent, 81, walletEvent(80000)) // −10000 a LATER unrelated drop (e.g. an order escrow)

	trades := svc.Trades()
	if len(trades) != 1 {
		t.Fatalf("expected exactly 1 instant buy, got %d", len(trades))
	}
	// Only the FIRST delta is the buy; the second must not leak in (net stays −10000).
	if trades[0].Net != -10_000 {
		t.Fatalf("instant buy net must be the single delta −10000, got %d", trades[0].Net)
	}
}

func TestSetupFee_SellAndBuyOrders(t *testing.T) {
	svc, p := newGlue(t)
	// Sell order (op 79): qty 1 × unit 179834 → fee 2.5% = 4496.
	p.dispatch(probe.KindRequest, 79, map[byte]interface{}{1: int32(1), 2: int32(555), 3: int64(1798340000), 4: int32(720)})
	// Buy order (op 80): qty 20 × unit 5326 = 106520 → fee 2.5% = 2663.
	p.dispatch(probe.KindRequest, 80, map[byte]interface{}{1: int32(752), 2: int32(20), 3: int32(1), 4: int32(720), 5: int64(53260000)})

	trades := svc.Trades()
	if len(trades) != 2 {
		t.Fatalf("expected 2 setup-fee rows, got %d", len(trades))
	}
	var sellFee, buyFee int64
	for _, tr := range trades {
		if tr.Source != model.TradeSourceSetup {
			t.Fatalf("expected setup source, got %q", tr.Source)
		}
		if tr.PartialAmount == 20 {
			buyFee = tr.SetupFee
		} else {
			sellFee = tr.SetupFee
		}
	}
	if sellFee != 4496 || buyFee != 2663 {
		t.Fatalf("setup fees wrong: sell=%d buy=%d", sellFee, buyFee)
	}
	// Setup fees flow into the summary's separate SetupFee total (FR-008).
	if sum := svc.TradeSummary(); sum.SetupFee != 4496+2663 || sum.Net != -(4496+2663) {
		t.Fatalf("setup summary wrong: %+v", sum)
	}
}

func TestInstant_QuicksellCapsAtItemCount(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindEvent, 81, walletEvent(1000)) // baseline
	p.dispatch(probe.KindRequest, 485, map[byte]interface{}{1: make([]int32, 2), 2: make([]int64, 2)}) // 2 items
	p.dispatch(probe.KindEvent, 81, walletEvent(1100)) // +100 item 1
	p.dispatch(probe.KindEvent, 81, walletEvent(1250)) // +150 item 2 → count reached, disarm
	p.dispatch(probe.KindEvent, 81, walletEvent(1900)) // +650 LATER income must NOT fold in

	trades := svc.Trades()
	if len(trades) != 1 || trades[0].Net != 250 {
		t.Fatalf("quicksell must cap at its 2-item count (net 250), got %+v", trades)
	}
}

func TestInstant_LoginSeedBaselineCorrelatesFirstDelta(t *testing.T) {
	svc, p := newGlue(t)
	// R:2 login seed sets the baseline; NO E:81 has arrived yet.
	p.seedWalletBaseline(1000)
	p.dispatch(probe.KindRequest, 315, map[byte]interface{}{1: int64(9), 2: int32(920), 4: int32(1)})
	p.dispatch(probe.KindEvent, 81, walletEvent(1561)) // the session's FIRST E:81 = the sale proceeds

	trades := svc.Trades()
	if len(trades) != 1 || trades[0].Net != 561 {
		t.Fatalf("a sale before the first live E:81 must still correlate (net 561), got %+v", trades)
	}
}

func TestInstant_UnrelatedWalletChangeIgnored(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindEvent, 81, walletEvent(1000)) // baseline
	p.dispatch(probe.KindEvent, 81, walletEvent(1500)) // +500 with NO armed trade (loot silver)
	if trades := svc.Trades(); len(trades) != 0 {
		t.Fatalf("unarmed wallet delta must not create a trade, got %d", len(trades))
	}
}
