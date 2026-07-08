// Package tradecalc holds the pure fee/tax/net breakdown math for the trade ledger
// (feature 017). Kept infrastructure-free so the accounting is golden-tested in
// isolation (Constitution III). All silver is real (÷10000 already applied upstream).
//
// The market withholds a sales tax from a sale. Two situations:
//   - Gross is known (a mail fill's total, or an instant trade whose order price was
//     cached) → tax and net are computed from Gross with the rate.
//   - Only Net is known (an instant trade, silver taken straight from the wallet delta,
//     post-tax) → Gross and tax are reconstructed from Net and the rate.
//
// The rate is a default (game constant, patch-dependent) unless a live observation gives
// an exact Gross+Net pair, in which case that exact split is used and TaxEstimated=false.
package tradecalc

import "math"

// DefaultSalesTaxRate is the market sales tax withheld from a sale. Albion's rate shifts
// with balance patches; this is the fallback when no exact Gross+Net observation exists.
// Observed live 2026-07-08 (instant sells) — refine from data when available.
const DefaultSalesTaxRate = 0.04

// Breakdown is the fee split for one sale/purchase.
type Breakdown struct {
	Gross     int64
	SalesTax  int64
	Net       int64
	Estimated bool // true when tax/gross was reconstructed from the rate, not observed
}

// SoldFromGross splits a known gross sale value: tax = gross × rate, net = gross − tax.
// Used for mail fills (the mail carries the pre-tax total). Estimated (rate-based).
func SoldFromGross(gross int64, rate float64) Breakdown {
	tax := int64(math.Round(float64(gross) * rate))
	return Breakdown{Gross: gross, SalesTax: tax, Net: gross - tax, Estimated: true}
}

// SoldFromNet reconstructs a sale from the observed wallet delta (post-tax net): gross =
// net / (1 − rate), tax = gross − net. Used for instant sells whose order price wasn't
// cached. Estimated (rate-based), but Net is exact (it is the wallet delta).
func SoldFromNet(net int64, rate float64) Breakdown {
	if rate <= 0 || rate >= 1 {
		return Breakdown{Gross: net, SalesTax: 0, Net: net, Estimated: true}
	}
	gross := int64(math.Round(float64(net) / (1 - rate)))
	return Breakdown{Gross: gross, SalesTax: gross - net, Net: net, Estimated: true}
}

// SoldExact splits a sale where BOTH gross (cached order price × amount) and net (wallet
// delta) are known — the tax is the real difference, no estimate.
func SoldExact(gross, net int64) Breakdown {
	return Breakdown{Gross: gross, SalesTax: gross - net, Net: net, Estimated: false}
}

// SetupFeeRate is the non-refundable fee charged when listing a marketplace order,
// as a fraction of the order value. Observed live 2026-07-08 (op 79/80): 2.5%.
const SetupFeeRate = 0.025

// SetupFee is the listing fee for an order of the given total value.
func SetupFee(orderValue int64) int64 {
	return int64(math.Round(float64(orderValue) * SetupFeeRate))
}

// Bought is a purchase: the buyer pays gross, no sales tax withheld (tax is on the
// seller). Net is negative (silver leaves the wallet).
func Bought(gross int64) Breakdown {
	return Breakdown{Gross: gross, SalesTax: 0, Net: -gross, Estimated: false}
}
