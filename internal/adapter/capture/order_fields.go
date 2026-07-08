package capture

// Order-placement extractors (feature 017 expansion): the setup fee paid when listing a
// marketplace order. REQUEST ops, live-decoded 2026-07-08:
//
//	Q:79 AuctionCreateOffer (list a SELL order):
//	  k1 = quantity, k2 = item object id (inventory), k3 = unit price ×10000.
//	Q:80 AuctionCreateRequest (list a BUY order):
//	  k1 = item type index, k2 = quantity, k5 = unit price ×10000.
//
// The setup fee is 2.5% of the order value (quantity × unit price) — computed from the
// request, so no wallet correlation is needed (a buy order's wallet delta also includes
// the refundable escrow, which is NOT a fee).

// SellOrderSetup reads a sell-order listing: item object id (k2), quantity (k1), unit
// price raw ×10000 (k3).
func SellOrderSetup(params map[byte]interface{}) (itemObj, qty int, unitRaw int64, ok bool) {
	q, ok1 := IntParam(params, 1)
	obj, ok2 := IntParam(params, 2)
	unit, ok3 := int64Val(params[3])
	if !ok1 || q <= 0 || !ok2 || !ok3 || unit <= 0 {
		return 0, 0, 0, false
	}
	return obj, q, unit, true
}

// BuyOrderSetup reads a buy-order listing: item type index (k1), quantity (k2), unit
// price raw ×10000 (k5).
func BuyOrderSetup(params map[byte]interface{}) (itemIndex, qty int, unitRaw int64, ok bool) {
	idx, ok1 := IntParam(params, 1)
	q, ok2 := IntParam(params, 2)
	unit, ok3 := int64Val(params[5])
	if !ok1 || idx <= 0 || !ok2 || q <= 0 || !ok3 || unit <= 0 {
		return 0, 0, 0, false
	}
	return idx, q, unit, true
}
