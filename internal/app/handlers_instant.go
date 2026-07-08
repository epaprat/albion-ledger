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
// amount (k4) are in the request; silver = the wallet increase that follows.
func handleInstantSell(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	idx, amount, ok := capture.InstantSell(params)
	if !ok {
		return
	}
	p.armInstant(&instantCtx{
		direction: model.TradeSold, source: model.TradeSourceInst,
		itemIndex: idx, amount: amount, single: true,
	})
}

// handleInstantBuy — Q:83: buy from a sell offer. Amount (k1) + order id (k2) are in the
// request; the item isn't (left unknown), and silver = the wallet decrease that follows.
func handleInstantBuy(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	amount, orderID, ok := capture.InstantBuy(params)
	if !ok {
		return
	}
	// The request carries only the order id; name the item from the offer cache (R:81)
	// when it was browsed, else leave it unknown (net is still exact from the wallet delta).
	itemID := p.offerCache[orderID]
	if p.debug {
		log.Printf("[trade] instant buy orderId=%d cacheSize=%d resolved=%q", orderID, len(p.offerCache), itemID)
	}
	p.armInstant(&instantCtx{
		direction: model.TradeBought, source: model.TradeSourceInst,
		itemID: itemID, amount: amount, single: true,
	})
}

// handleQuicksell — Q:485: quicksell a batch to buy orders. The batch size (len of k1) is
// the item count; silver = the sum of the wallet increases that follow (aggregate row).
func handleQuicksell(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	count, ok := capture.Quicksell(params)
	if !ok {
		return
	}
	p.armInstant(&instantCtx{
		direction: model.TradeSold, source: model.TradeSourceQuick,
		count: count,
	})
}
