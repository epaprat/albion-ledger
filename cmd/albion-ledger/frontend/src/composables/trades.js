// Pure display helpers for the Trades panel (017). Kept out of the SFC so the P&L
// display logic is unit-tested without a component-mount harness (mirrors useTable.js).

// dirLabel maps a trade direction to its column label.
export const dirLabel = (d) => (d === 'bought' ? 'Bought' : 'Sold')

// netClass picks the colour class for a net figure (income − expense).
export const netClass = (net) => (net >= 0 ? 'pos' : 'neg')

// amountText shows "filled / total" only for a partially-filled (expired) order; a fully
// completed order shows just the amount. fmt is the number formatter (injected for tests).
export function amountText(r, fmt) {
  return r.partialAmount !== r.totalAmount
    ? `${fmt(r.partialAmount)} / ${fmt(r.totalAmount)}`
    : fmt(r.partialAmount)
}

// tradeFields are the free-text filter accessors (item name, wire id, direction).
export const tradeFields = [(r) => r.itemName, (r) => r.itemId, (r) => r.direction]

// tradeAccessor maps a sort column key to a trade's comparable value.
export const tradeAccessor = (r, k) => ({
  direction: r.direction || '',
  item: r.itemName || r.itemId || '',
  amount: r.partialAmount || 0,
  gross: r.gross || 0,
  setup: r.setupFee || 0,
  tax: r.salesTax || 0,
  net: r.net || 0,
  received: r.received || 0,
}[k])

// sourceLabel names how a trade was captured.
export const sourceLabel = (s) => ({ mail: 'Order', instant: 'Instant', quicksell: 'Quicksell', setup: 'Setup fee' }[s] || s || '')
