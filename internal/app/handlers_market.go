package app

// Market order handlers (feature 010): sell/buy order lists are the wire's only
// real MARKET price source — the missing piece for pricing resources (the K bank
// overview reports 0 for them and resource declarations carry no EMV). Browsing
// the market in game now feeds the price book (and the persisted estimate book),
// so bank summary rows price without ever being declared.

import (
	"log"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func init() {
	register(model.CatMarketSellOrders, handleMarketOrders)
	register(model.CatMarketBuyOrders, handleMarketOrders)
}

// handleMarketOrders feeds SELL-order (offer) unit prices into valuation. Buy
// orders (requests) are ignored for pricing: a buyer's bid inflates nothing —
// the asking price is the honest "what is this worth" figure.
func handleMarketOrders(p *Pipeline, _ probe.Kind, code int, params map[byte]interface{}) {
	orders, ok := capture.MarketOrders(params)
	if !ok {
		return
	}
	fed := 0
	for _, o := range orders {
		if !o.IsOffer {
			continue
		}
		p.sink.IngestMarketPrice(o.UniqueName, o.Quality, o.UnitRaw/vaultValueScale)
		fed++
	}
	if p.debug && fed > 0 {
		log.Printf("[market] orders code=%d n=%d offers-fed=%d", code, len(orders), fed)
	}
}
