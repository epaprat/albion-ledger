// Number + item-label formatting shared across the UI.

// fmt: full grouped integer (e.g. 568,782) — used for counts/quantities.
export const fmt = (n) => (n || 0).toLocaleString('en-US')

const UNITS = ['k', 'M', 'B', 'T']
const trim = (x) => x.toFixed(1).replace(/\.0$/, '')

// compact: in-game-style short value (e.g. 569k, 7.2M, 33.7M) — used for silver.
// Promotes correctly at boundaries (999,600 → "1.0M", not "1000k").
export function compact(n) {
  n = n || 0
  const neg = n < 0 ? '-' : ''
  let a = Math.abs(n)
  if (a < 1000) return neg + Math.round(a)
  let u = -1
  while (a >= 1000 && u < UNITS.length - 1) { a /= 1000; u++ }
  let s = u === 0 ? Math.round(a) : Math.round(a * 10) / 10
  if (s >= 1000 && u < UNITS.length - 1) { s = Math.round((s / 1000) * 10) / 10; u++ }
  return neg + (u === 0 ? String(s) : trim(s)) + UNITS[u]
}

// Item display helpers (one source of truth for list + grid + market).
export const tierLabel = (it) => (it && it.tier ? `T${it.tier}${it.enchant ? '.' + it.enchant : ''}` : '—')
export const qLabel = (q) => (q ? ['', 'Normal', 'Good', 'Outstanding', 'Excellent', 'Masterpiece'][q] : '—')
export const srcText = { live_market: 'live', server_estimate: 'est', unknown: '—' }

// dur: compact human duration from ms (e.g. "1h 12m", "3m", "45s") for session length.
export function dur(ms) {
  const s = Math.floor((ms || 0) / 1000)
  if (s < 60) return s + 's'
  const m = Math.floor(s / 60)
  if (m < 60) return m + 'm'
  const h = Math.floor(m / 60)
  return h + 'h ' + (m % 60) + 'm'
}
