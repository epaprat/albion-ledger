import { describe, it, expect } from 'vitest'
import { dirLabel, netClass, amountText, tradeAccessor, tradeFields, realizedWindows, windowLabel } from './trades.js'
import { sortRows, filterRows } from './useTable.js'

const fmt = (n) => String(n)

describe('dirLabel', () => {
  it('maps directions to labels', () => {
    expect(dirLabel('sold')).toBe('Sold')
    expect(dirLabel('bought')).toBe('Bought')
  })
})

describe('netClass', () => {
  it('positive/zero net is pos, negative is neg', () => {
    expect(netClass(400)).toBe('pos')
    expect(netClass(0)).toBe('pos')
    expect(netClass(-10)).toBe('neg')
  })
})

describe('amountText', () => {
  it('shows just the amount for a fully-filled order', () => {
    expect(amountText({ partialAmount: 5, totalAmount: 5 }, fmt)).toBe('5')
  })
  it('shows filled / total for a partially-filled (expired) order', () => {
    expect(amountText({ partialAmount: 12, totalAmount: 39 }, fmt)).toBe('12 / 39')
  })
})

describe('realizedWindows (018)', () => {
  it('exposes the four window options in order with backend-matching values', () => {
    expect(realizedWindows.map((o) => o.value)).toEqual(['session', 'today', '7d', 'all'])
  })
  it('windowLabel maps a value to its human label, falling back to the raw value', () => {
    expect(windowLabel('7d')).toBe('Last 7 days')
    expect(windowLabel('all')).toBe('All time')
    expect(windowLabel('bogus')).toBe('bogus')
  })
})

describe('trades table integration', () => {
  const rows = [
    { tradeId: 'a', direction: 'sold', itemName: 'Ashenbark Logs', itemId: 'T7_WOOD', partialAmount: 5, gross: 50000, salesTax: 2000, net: 48000, received: 30 },
    { tradeId: 'b', direction: 'bought', itemName: 'Panther Cub', itemId: 'T5_ALCHEMY', partialAmount: 23, gross: 1955000, net: -1955000, received: 10 },
    { tradeId: 'c', direction: 'sold', itemName: 'Horn', itemId: 'T6_OFF_HORN', partialAmount: 6, gross: 442068, salesTax: 17683, net: 424385, received: 20 },
  ]

  it('default sort by received desc = newest first', () => {
    expect(sortRows(rows, 'received', 'desc').map((r) => r.tradeId)).toEqual(['a', 'c', 'b'])
  })
  it('sorts by gross', () => {
    expect(sortRows(rows, 'gross', 'desc', (r, k) => tradeAccessor(r, k)).map((r) => r.tradeId)).toEqual(['b', 'c', 'a'])
  })
  it('sorts by net (expense negative sinks)', () => {
    expect(sortRows(rows, 'net', 'desc', (r, k) => tradeAccessor(r, k)).map((r) => r.tradeId)).toEqual(['c', 'a', 'b'])
  })
  it('filters by item name across fields', () => {
    expect(filterRows(rows, 'panther', tradeFields).map((r) => r.tradeId)).toEqual(['b'])
  })
  it('filters by direction', () => {
    expect(filterRows(rows, 'sold', tradeFields).map((r) => r.tradeId)).toEqual(['a', 'c'])
  })
})
