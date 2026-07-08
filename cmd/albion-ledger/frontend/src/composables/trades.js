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

// realizedWindows is the single source of the realized-P&L window options (018). The value
// must match the backend TradeSummary(window) keys; the label is what the player reads.
export const realizedWindows = [
  { value: 'session', label: 'This session' },
  { value: 'today', label: 'Today' },
  { value: '7d', label: 'Last 7 days' },
  { value: 'all', label: 'All time' },
]

// windowLabel maps a window value to its human label (falls back to the raw value).
export const windowLabel = (w) => (realizedWindows.find((o) => o.value === w) || {}).label || w

// netText renders a net figure, prefixing "~" when the trade's net could not be verified
// against its expected order value (018) — an honest "estimated" marker, not a false exact.
export function netText(r, fmt) {
  return (r.netEstimated ? '~' : '') + fmt(r.net)
}
