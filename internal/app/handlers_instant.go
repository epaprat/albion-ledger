package app

// Instant marketplace trade handlers (feature 017 expansion): the player's own instant
// sell / instant buy / quicksell REQUEST ops. Each carries the item + amount but not the
// silver — it arms a pending context that the following wallet delta (E:81) fills in
// (see pipeline correlateWallet). Registry pattern (009): register only; dispatch.go
// untouched.

import (
	"log"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func init() {
	register(model.CatInstantSell, handleInstantSell)
	register(model.CatInstantBuy, handleInstantBuy)
	register(model.CatQuicksell, handleQuicksell)
}

// handleInstantSell — Q:315: sell a specific item to a buy order. Item type index (k2) +
// amount (k4) + the target buy order id (k1) are in the request; silver = the wallet
// increase that follows. When the buy order was browsed (its price cached), an expected
// gross gates the delta on magnitude (018), else the 017 sign-only path applies.
func handleInstantSell(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	idx, amount, orderID, ok := capture.InstantSell(params)
	if !ok {
		return
	}
	expected, known := p.orderValue(orderID, amount)
	p.armInstant(&instantCtx{
		direction: model.TradeSold, source: model.TradeSourceInst,
		itemIndex: idx, amount: amount, single: true,
		expectedGross: expected, expectedKnown: known,
	})
}

// handleInstantBuy — Q:83: buy from a sell offer. Amount (k1) + order id (k2) are in the
// request; the item + price come from the cached offer (R:82) when it was browsed. A buy
// pays the offer price exactly, so its expected gross gates the delta tightly (018).
func handleInstantBuy(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	amount, orderID, ok := capture.InstantBuy(params)
	if !ok {
		return
	}
	var itemID string
	if oi, found := p.orderInfoFor(orderID); found {
		itemID = oi.name
	}
	expected, known := p.orderValue(orderID, amount)
	if p.debug {
		log.Printf("[trade] instant buy orderId=%d cacheSize=%d resolved=%q expected=%d known=%t", orderID, p.offerCache.Len(), itemID, expected, known)
	}
	p.armInstant(&instantCtx{
		direction: model.TradeBought, source: model.TradeSourceInst,
		itemID: itemID, amount: amount, single: true,
		expectedGross: expected, expectedKnown: known,
	})
}

// handleQuicksell — Q:485: quicksell a batch to buy orders. The batch size (len of k1) is
// the item count; silver = the sum of the wallet increases that follow (aggregate row).
// Per-order quantities are NOT on the wire, so a precise expected value can't be built;
// quicksell keeps the 017 count-capped sign correlation (already bounded). [DEFERRED: soft
// expected bracket once per-order fill quantities are decoded.]
func handleQuicksell(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	count, _, ok := capture.Quicksell(params)
	if !ok {
		return
	}
	p.armInstant(&instantCtx{
		direction: model.TradeSold, source: model.TradeSourceQuick,
		count: count,
	})
}
