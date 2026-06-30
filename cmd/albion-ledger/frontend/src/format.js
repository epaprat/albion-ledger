// Number formatting shared across the UI.

// fmt: full grouped integer (e.g. 568,782) — used for counts/quantities.
export const fmt = (n) => (n || 0).toLocaleString('en-US')

// compact: in-game-style short value (e.g. 569k, 7.2M, 33.7M) — used for silver.
export function compact(n) {
  n = n || 0
  const a = Math.abs(n)
  if (a >= 1e9) return trim(n / 1e9) + 'B'
  if (a >= 1e6) return trim(n / 1e6) + 'M'
  if (a >= 1e3) return Math.round(n / 1e3) + 'k'
  return String(Math.round(n))
}

const trim = (x) => x.toFixed(1).replace(/\.0$/, '')
