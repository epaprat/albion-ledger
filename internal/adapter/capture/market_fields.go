package capture

// Market order extractors (feature 010): sell/buy order lists (R:82/R:81) arrive as
// JSON strings — the only place the wire hands us real MARKET prices, which is the
// missing price source for resources (the K overview reports 0 for them and
// resource declarations carry no EMV). Live-decoded 2026-07-05 (kb.pcap):
//
//	{"UnitPriceSilver":70000, "ItemTypeId":"T4_RUNE", "EnchantmentLevel":0,
//	 "QualityLevel":1, "AuctionType":"offer"|"request", ...}
//
// Untrusted input: every row parses independently; malformed rows are skipped, the list
// is size-capped, and prices must be positive (Principle IV).

import (
	"encoding/json"
	"strconv"
)

const maxMarketOrders = 128

// MarketOrder is one parsed order row.
type MarketOrder struct {
	OrderID    int64  // marketplace order id (for instant-buy item resolution, 017)
	UniqueName string // ItemTypeId with "@N" appended for enchanted items
	Quality    int
	UnitRaw    int64 // silver ×10000
	IsOffer    bool  // sell order (offer) vs buy order (request)
}

type wireOrder struct {
	ID               int64  `json:"Id"`
	UnitPriceSilver  int64  `json:"UnitPriceSilver"`
	ItemTypeID       string `json:"ItemTypeId"`
	EnchantmentLevel int    `json:"EnchantmentLevel"`
	QualityLevel     int    `json:"QualityLevel"`
	AuctionType      string `json:"AuctionType"`
}

// MarketOrders parses an order-list response (key 0 = []string of JSON rows).
func MarketOrders(params map[byte]interface{}) ([]MarketOrder, bool) {
	rows, ok := params[0].([]string)
	if !ok || len(rows) == 0 {
		return nil, false
	}
	if len(rows) > maxMarketOrders {
		rows = rows[:maxMarketOrders]
	}
	out := make([]MarketOrder, 0, len(rows))
	for _, r := range rows {
		var w wireOrder
		if err := json.Unmarshal([]byte(r), &w); err != nil {
			continue // hostile/partial row — skip, keep the rest
		}
		if w.ItemTypeID == "" || w.UnitPriceSilver <= 0 {
			continue
		}
		name := w.ItemTypeID
		if w.EnchantmentLevel > 0 {
			name += "@" + strconv.Itoa(w.EnchantmentLevel)
		}
		q := w.QualityLevel
		if q < 0 || q > 5 {
			q = 0
		}
		out = append(out, MarketOrder{
			OrderID:    w.ID,
			UniqueName: name,
			Quality:    q,
			UnitRaw:    w.UnitPriceSilver,
			IsOffer:    w.AuctionType == "offer",
		})
	}
	return out, len(out) > 0
}
