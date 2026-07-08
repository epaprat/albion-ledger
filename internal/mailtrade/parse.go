// Package mailtrade decodes the body of an Albion marketplace order-fill mail into a
// structured trade. It is pure (no wire/DB/infra deps) so the five body layouts are
// golden-tested in isolation (Constitution III). The app layer maps a Parsed result to
// a domain model.Trade, adding the mail id / item name / timestamp it holds.
//
// Body formats (silver is fixed-point ×10000 on the wire, like every other silver):
//
//	SELLORDER_FINISHED   AMOUNT|ITEM_ID|TOTAL_SILVER|UNIT_SILVER      (item idx1) sold
//	BUYORDER_FINISHED    AMOUNT|ITEM_ID|TOTAL_SILVER|UNIT_SILVER      (item idx1) bought
//	SELLORDER_EXPIRED    SOLD|TOTAL|TOTAL_SILVER|ITEM_ID              (item idx3) sold
//	BUYORDER_EXPIRED     BOUGHT|TOTAL|TOTAL_REFUND|ITEM_ID            (item idx3) bought
//	BLACKMARKET_SELLORDER_EXPIRED  SOLD|TOTAL|TOTAL_SILVER|ITEM_ID    (item idx3) sold
//
// Reference: clients/AlbionDataAvalonia AlbionMail.GetData (the richest of the two
// vendored clients — it covers all five types; the Go client omits buy orders).
package mailtrade

import (
	"math"
	"strconv"
	"strings"
)

// silverScale is the wire fixed-point divisor for silver.
const silverScale = 10000

// MailType is a marketplace mail-info type. UNKNOWN = a non-trade mail (ignored).
type MailType int

const (
	Unknown MailType = iota
	SellOrderFinished
	BuyOrderFinished
	SellOrderExpired
	BuyOrderExpired
	BlackmarketSellOrderExpired
)

// Directions.
const (
	Sold   = "sold"
	Bought = "bought"
)

// TypeFromString maps the wire type string (the Types array in GetMailInfos) to a
// MailType. Unrecognized → Unknown (the mail is not a marketplace trade).
func TypeFromString(s string) MailType {
	switch s {
	case "MARKETPLACE_SELLORDER_FINISHED_SUMMARY":
		return SellOrderFinished
	case "MARKETPLACE_BUYORDER_FINISHED_SUMMARY":
		return BuyOrderFinished
	case "MARKETPLACE_SELLORDER_EXPIRED_SUMMARY":
		return SellOrderExpired
	case "MARKETPLACE_BUYORDER_EXPIRED_SUMMARY":
		return BuyOrderExpired
	case "BLACKMARKET_SELLORDER_EXPIRED_SUMMARY":
		return BlackmarketSellOrderExpired
	default:
		return Unknown
	}
}

// Parsed is the trade content a mail body yields. The app layer adds MailID/ItemName/
// Received/Location around it.
type Parsed struct {
	Direction     string // Sold | Bought
	ItemID        string // wire uniqueName (with @enchant)
	PartialAmount int    // amount actually filled
	TotalAmount   int    // order total (FINISHED: == PartialAmount)
	TotalSilver   int64  // real silver (÷10000)
	UnitSilver    float64
}

// ParseBody decodes a mail body for its type. Returns ok=false when the type is Unknown,
// a required field is missing, or a number won't parse — the caller drops that mail
// (no crash, no fabricated row).
func ParseBody(t MailType, body string) (Parsed, bool) {
	parts := strings.Split(body, "|")
	// Every trade layout uses indices 0..3, so at least four fields must be present.
	if t == Unknown || len(parts) < 4 {
		return Parsed{}, false
	}

	switch t {
	case SellOrderFinished, BuyOrderFinished:
		amount, ok1 := atoi(parts[0])
		total, ok2 := atoi64(parts[2])
		if !ok1 || !ok2 {
			return Parsed{}, false
		}
		totalSilver := total / silverScale
		p := Parsed{ItemID: parts[1], PartialAmount: amount, TotalAmount: amount, TotalSilver: totalSilver}
		if t == SellOrderFinished {
			unit, ok3 := atoi64(parts[3])
			if !ok3 {
				return Parsed{}, false
			}
			p.Direction = Sold
			p.UnitSilver = round2(float64(unit) / silverScale)
		} else {
			// A completed buy exposes the actual total paid — derive unit from it, not the
			// original limit price.
			p.Direction = Bought
			p.UnitSilver = round2(perUnit(totalSilver, amount))
		}
		return p, true

	case SellOrderExpired, BlackmarketSellOrderExpired:
		sold, ok1 := atoi(parts[0])
		totalAmt, ok2 := atoi(parts[1])
		total, ok3 := atoi64(parts[2])
		if !ok1 || !ok2 || !ok3 {
			return Parsed{}, false
		}
		totalSilver := total / silverScale
		return Parsed{
			Direction: Sold, ItemID: parts[3], PartialAmount: sold, TotalAmount: totalAmt,
			TotalSilver: totalSilver, UnitSilver: round2(perUnit(totalSilver, sold)),
		}, true

	case BuyOrderExpired:
		// Expired buy orders don't expose the real paid price for filled items; the
		// returned silver (refund of the unfilled remainder) infers the limit price.
		bought, ok1 := atoi(parts[0])
		totalAmt, ok2 := atoi(parts[1])
		refundRaw, ok3 := atoi64(parts[2])
		if !ok1 || !ok2 || !ok3 {
			return Parsed{}, false
		}
		refund := float64(refundRaw) / silverScale
		remaining := totalAmt - bought
		if remaining <= 0 {
			// Price is inferred from the refund of the UNFILLED remainder; a fully-filled
			// expired order exposes none, so drop it rather than record a free purchase
			// (a fully-filled order normally arrives as FINISHED anyway). Review.
			return Parsed{}, false
		}
		unit := refund / float64(remaining)
		unit = round2(unit)
		total := int64(math.Round(unit * float64(bought)))
		return Parsed{
			Direction: Bought, ItemID: parts[3], PartialAmount: bought, TotalAmount: totalAmt,
			TotalSilver: total, UnitSilver: unit,
		}, true
	}
	return Parsed{}, false
}

// perUnit is total/amount guarding a zero amount (an expired order can fill zero).
func perUnit(total int64, amount int) float64 {
	if amount == 0 {
		return 0
	}
	return float64(total) / float64(amount)
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}

func atoi64(s string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n, err == nil
}
