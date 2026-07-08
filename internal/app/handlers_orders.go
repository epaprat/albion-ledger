package app

// Order-listing handlers (feature 017 expansion): the player lists a sell/buy order and
// pays a non-refundable 2.5% setup fee. The fee is computed from the order value in the
// request — no wallet correlation needed (a buy order's wallet delta also holds the
// refundable escrow). Listing also clears any pending instant context so the escrow/fee
// delta can't leak into a prior instant trade. Registry pattern (009): register only.

import (
	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/tradecalc"
)

func init() {
	register(model.CatSellOrder, handleSellOrder)
	register(model.CatBuyOrder, handleBuyOrder)
}

// handleSellOrder — Q:79: list a sell order. Item object id (k2) resolves via the object
// registry when known; qty (k1) × unit price (k3) is the order value for the fee.
func handleSellOrder(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	itemObj, qty, unitRaw, ok := capture.SellOrderSetup(params)
	if !ok {
		return
	}
	p.clearInstant()
	idx := 0
	if ref, ok := p.resolveObj(itemObj); ok {
		idx = ref.Index
	}
	value := int64(qty) * unitRaw / silverScale
	p.emitSetupTrade(idx, qty, tradecalc.SetupFee(value))
}

// handleBuyOrder — Q:80: list a buy order. Item type index (k1), qty (k2) × unit price
// (k5) is the order value for the fee.
func handleBuyOrder(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	idx, qty, unitRaw, ok := capture.BuyOrderSetup(params)
	if !ok {
		return
	}
	p.clearInstant()
	value := int64(qty) * unitRaw / silverScale
	p.emitSetupTrade(idx, qty, tradecalc.SetupFee(value))
}
