package app

// Wallet + net worth goldens (016) through the real dispatch path.

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func TestWalletSetsNetWorth(t *testing.T) {
	svc, p := newGlue(t)
	// E:81 wallet = 8.47e9 ×10000 → 847,000 silver.
	p.dispatch(probe.KindEvent, 81, map[byte]interface{}{1: int64(8_470_000_000), 252: int16(81)})
	sum := svc.HoldingsSummary()
	if !sum.WalletKnown || sum.WalletSilver != 847_000 {
		t.Fatalf("wallet not set: known=%v silver=%d", sum.WalletKnown, sum.WalletSilver)
	}
	// No holdings value in this test → net worth == wallet.
	if sum.NetWorth != 847_000+sum.TotalValue {
		t.Fatalf("net worth must be wallet + holdings: %d", sum.NetWorth)
	}
}

func TestWalletNewestWins(t *testing.T) {
	svc, p := newGlue(t)
	// Login seed via R:2 (k33), then a fresher live E:81 — E:81 must win.
	p.nowMS = func() int64 { return 1000 }
	p.dispatch(probe.KindResponse, 2, map[byte]interface{}{55: []int16{}, 33: int64(1_000_0000), 252: int16(2)})
	p.nowMS = func() int64 { return 2000 }
	p.dispatch(probe.KindEvent, 81, map[byte]interface{}{1: int64(5_000_0000), 252: int16(81)})
	if s := svc.HoldingsSummary().WalletSilver; s != 5_000 {
		t.Fatalf("newest source must win: got %d, want 5000", s)
	}
}

func TestNetWorthHonestWhenWalletUnknown(t *testing.T) {
	svc, _ := newGlue(t)
	sum := svc.HoldingsSummary()
	if sum.WalletKnown {
		t.Fatal("wallet must be unknown before any wallet event")
	}
	// No fake zero wallet in net worth — it equals holdings value only (FR-003).
	if sum.NetWorth != sum.TotalValue {
		t.Fatalf("unknown wallet: net worth must equal holdings value, got %d vs %d", sum.NetWorth, sum.TotalValue)
	}
}
