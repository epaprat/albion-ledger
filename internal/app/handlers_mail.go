package app

// Mail handlers (feature 017): the marketplace order-fill P&L. Two RESPONSE messages,
// two-step:
//
//	R:174 GetMailInfos → cache each mail id → its type (needed to parse the body).
//	R:176 ReadMail     → look the type up, parse the body, emit a Trade.
//
// A ReadMail whose GetMailInfos row was never seen is dropped (the type is unknown, so
// the body can't be parsed) — the honest passive limit (FR-004). Registry pattern (009):
// two register calls, dispatch.go untouched.

import (
	"fmt"
	"log"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/mailtrade"
	"github.com/epaprat/albion-ledger/internal/tradecalc"
)

func init() {
	register(model.CatMailInfos, handleMailInfos)
	register(model.CatMailRead, handleMailRead)
}

// handleMailInfos — R:174: cache the id→type/location/received rows for later reads.
func handleMailInfos(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	p.ingestMailInfos(params)
}

// mailReceived normalizes the wire mail timestamp to unix ms, falling back to the given
// capture time when it isn't usable.
func mailReceived(raw, fallback int64) int64 {
	if ms := capture.MailReceivedMs(raw); ms > 0 {
		return ms
	}
	return fallback
}

// ingestMailInfos caches + persists a GetMailInfos list when params carry the mail-list
// shape (a MARKETPLACE_/BLACKMARKET_ type array). Returns true when it was mail-shaped.
// The list arrives as R:174 for a small mailbox, but as a bulk R:1 sync for a large one
// (op 1 is SHARED with bank tab content — live-decoded 2026-07-08); both codes delegate
// here and the content signature decides. Produces no trade — only fills the type cache
// that ReadMail (R:176) needs.
func (p *Pipeline) ingestMailInfos(params map[byte]interface{}) bool {
	infos, ok := capture.MailInfos(params)
	if !ok {
		return false
	}
	for _, mi := range infos {
		p.putMailInfo(mi.ID, mailInfoEntry{typ: mi.Type, location: mi.LocationID, received: mi.Received})
		p.sink.SaveMailInfo(mi.ID, mi.Type, mi.LocationID, mi.Received) // persist across sessions
	}
	if p.debug {
		log.Printf("[mail] infos cached: %d", len(infos))
	}
	return true
}

// handleMailRead — R:176: parse an opened mail's body against its cached type and emit
// the resulting Trade. Unknown type (infos not seen) or an unparsable body → dropped.
func handleMailRead(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	id, body, ok := capture.ReadMail(params)
	if !ok {
		return
	}
	info, known := p.getMailInfo(id)
	if !known {
		if p.debug {
			log.Printf("[mail] read %d dropped: type unknown (infos not seen this session)", id)
		}
		return
	}
	parsed, ok := mailtrade.ParseBody(mailtrade.TypeFromString(info.typ), body)
	if !ok {
		return
	}
	// The mail carries the gross (pre-tax) total; the fill's wallet delta isn't available
	// at read time, so tax/net are rate-derived (TaxEstimated). A buy withholds no tax.
	var bd tradecalc.Breakdown
	if parsed.Direction == mailtrade.Sold {
		bd = tradecalc.SoldFromGross(parsed.TotalSilver, tradecalc.DefaultSalesTaxRate)
	} else {
		bd = tradecalc.Bought(parsed.TotalSilver)
	}
	p.sink.AddTrade(model.Trade{
		TradeID:       fmt.Sprintf("mail:%d", id),
		Direction:     parsed.Direction,
		Source:        model.TradeSourceMail,
		ItemID:        parsed.ItemID,
		PartialAmount: parsed.PartialAmount,
		TotalAmount:   parsed.TotalAmount,
		Gross:         bd.Gross,
		SalesTax:      bd.SalesTax,
		Net:           bd.Net,
		TaxEstimated:  bd.Estimated,
		UnitSilver: parsed.UnitSilver,
		// Order by the real sale time from the mail (normalized to unix ms) so a recent
		// order-fill sorts by when it SOLD, not when the mail was opened; fall back to
		// capture time when the wire carries no usable timestamp.
		Received:   mailReceived(info.received, p.nowMS()),
		LocationID: info.location,
	})
	if p.debug {
		log.Printf("[mail] trade: %s %s x%d gross=%d net=%d", parsed.Direction, parsed.ItemID, parsed.PartialAmount, bd.Gross, bd.Net)
	}
}
