package capture

// Instant marketplace trade extractors (feature 017 expansion). These are REQUEST ops —
// the player's own outgoing action. They carry WHAT was traded but NOT the silver; the
// silver is the wallet delta (E:81) that follows, correlated in the pipeline.
//
// Wire shapes (live sniff 2026-07-08):
//
//	Q:315 AuctionSellSpecificItemRequest (instant sell to a buy order):
//	  k2 = item TYPE index, k4 = amount. (k1 = order id.)
//	Q:83  AuctionBuyOffer (instant buy from a sell offer):
//	  k1 = amount, k2 = order id. (No item — resolved from the offer, or left unknown.)
//	Q:485 QuickSellAuctionSellAction (quicksell a batch to buy orders):
//	  k1 = []item object ids (batch size), k2 = []order ids.

// InstantSell reads an instant-sell request: item type index (k2) + amount (k4) + the buy
// order id it sells INTO (k1), used to price the expected value from the cached buy order
// (018). orderID is 0 when absent — the caller then falls back to sign-only correlation.
func InstantSell(params map[byte]interface{}) (itemIndex, amount int, orderID int64, ok bool) {
	idx, iok := IntParam(params, 2)
	amt, aok := IntParam(params, 4)
	if !iok || idx <= 0 || !aok || amt <= 0 {
		return 0, 0, 0, false
	}
	oid, _ := int64Val(params[1]) // order id optional; 0 → expected value unknown
	return idx, amt, oid, true
}

// InstantBuy reads an instant-buy request: amount (k1) + order id (k2). The item is not
// in the request; the caller resolves it from an offer cache or leaves it unknown.
func InstantBuy(params map[byte]interface{}) (amount int, orderID int64, ok bool) {
	amt, aok := IntParam(params, 1)
	oid, ook := int64Val(params[2])
	if !aok || amt <= 0 || !ook {
		return 0, 0, false
	}
	return amt, oid, true
}

// Quicksell reads a quicksell request: the count of items in the batch (len of the k1
// item-object array) and the buy order ids it sells into (k2), used to price the expected
// batch value (018). Silver is the sum of the following wallet deltas (aggregate row).
// orderIDs is nil when absent — the caller then falls back to sign-only correlation.
func Quicksell(params map[byte]interface{}) (count int, orderIDs []int64, ok bool) {
	n := sliceLen(params[1])
	if n <= 0 {
		return 0, nil, false
	}
	return n, int64Slice(params[2]), true
}

// sliceLen returns the element count of a wire array of any width, else 0.
func sliceLen(v interface{}) int {
	switch a := v.(type) {
	case []int:
		return len(a)
	case []int16:
		return len(a)
	case []int32:
		return len(a)
	case []int64:
		return len(a)
	case []byte:
		return len(a)
	case []string:
		return len(a)
	default:
		return 0
	}
}
