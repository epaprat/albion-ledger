import { describe, it, expect } from 'vitest'
import { ref } from 'vue'
import { sortRows, filterRows, useTable } from './useTable.js'

const rows = [
  { name: 'Bear Paws', value: 300 },
  { name: 'battleaxe', value: 100 },
  { name: 'Greataxe', value: 300 },
]

describe('sortRows', () => {
  it('null key returns source order untouched', () => {
    expect(sortRows(rows, null, 'desc').map((r) => r.name)).toEqual(['Bear Paws', 'battleaxe', 'Greataxe'])
  })
  it('sorts numbers desc and is stable on ties', () => {
    // 300s tie → keep original order (Bear Paws before Greataxe).
    expect(sortRows(rows, 'value', 'desc').map((r) => r.name)).toEqual(['Bear Paws', 'Greataxe', 'battleaxe'])
  })
  it('sorts numbers asc', () => {
    expect(sortRows(rows, 'value', 'asc').map((r) => r.value)).toEqual([100, 300, 300])
  })
  it('sorts text case-insensitively', () => {
    expect(sortRows(rows, 'name', 'asc').map((r) => r.name)).toEqual(['battleaxe', 'Bear Paws', 'Greataxe'])
  })
  it('does not mutate the input', () => {
    const before = rows.map((r) => r.name)
    sortRows(rows, 'value', 'asc')
    expect(rows.map((r) => r.name)).toEqual(before)
  })
})

describe('filterRows', () => {
  const fields = [(r) => r.name]
  it('empty query keeps everything', () => {
    expect(filterRows(rows, '', fields)).toHaveLength(3)
  })
  it('matches case-insensitive substring', () => {
    expect(filterRows(rows, 'AXE', fields).map((r) => r.name)).toEqual(['battleaxe', 'Greataxe'])
  })
  it('no match yields empty', () => {
    expect(filterRows(rows, 'zzz', fields)).toHaveLength(0)
  })
})

describe('useTable', () => {
  it('toggleSort cycles: sort → flip → clear', () => {
    const t = useTable(ref(rows), { fields: [(r) => r.name] })
    t.toggleSort('value')
    expect([t.sortKey.value, t.sortDir.value]).toEqual(['value', 'desc'])
    t.toggleSort('value')
    expect(t.sortDir.value).toBe('asc')
    t.toggleSort('value')
    expect(t.sortKey.value).toBe(null)
  })

  it('preserves sort/filter across a source list swap (capture tick, FR-005)', () => {
    const src = ref(rows)
    const t = useTable(src, { fields: [(r) => r.name] })
    t.toggleSort('value') // value desc
    t.query.value = 'axe'
    // A live tick replaces the array with a fresh reference.
    src.value = [
      { name: 'Halberd', value: 50 },
      { name: 'Battleaxe', value: 500 },
    ]
    expect(t.sortKey.value).toBe('value') // state survived
    expect(t.query.value).toBe('axe')
    expect(t.visibleRows.value.map((r) => r.name)).toEqual(['Battleaxe']) // re-derived
    expect(t.shown.value).toBe(1)
    expect(t.total.value).toBe(2)
  })

  it('clearFilter empties the query', () => {
    const t = useTable(ref(rows), { fields: [(r) => r.name] })
    t.query.value = 'x'
    t.clearFilter()
    expect(t.query.value).toBe('')
  })
})
