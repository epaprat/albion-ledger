import { describe, it, expect } from 'vitest'
import { earningsCards, earningsHero } from './earnings.js'

const summary = {
  selfKnown: true, active: true, rateReady: true, elapsedMs: 600000,
  netSilver: 1070, silverPerHour: 6420, nowPerHour: 5000,
  silverValue: 1000, silverAvgPerHour: 6000, silverNowPerHour: 4800,
  lootValue: 30, lootAvgPerHour: 180, lootNowPerHour: 150,
  gatherValue: 40, gatherAvgPerHour: 240, gatherNowPerHour: 200,
  fame: 5000, famePerHour: 30000, fameNowPerHour: 28000,
}

describe('earningsCards', () => {
  it('produces four streams in order with total/now/avg', () => {
    const c = earningsCards(summary)
    expect(c.map((x) => x.key)).toEqual(['silver', 'loot', 'gather', 'fame'])
    expect(c[0]).toMatchObject({ total: 1000, now: 4800, avg: 6000, isFame: false })
    expect(c[1]).toMatchObject({ total: 30, now: 150, avg: 180 })
    expect(c[3]).toMatchObject({ label: 'Fame', total: 5000, now: 28000, avg: 30000, isFame: true })
  })

  it('silver card is silver-only, never loot/gather or fame', () => {
    const c = earningsCards(summary)
    expect(c[0].total).toBe(1000) // not netSilver (1070) and not including fame
  })

  it('handles a missing/empty summary as zeros', () => {
    const c = earningsCards(undefined)
    expect(c).toHaveLength(4)
    expect(c.every((x) => x.total === 0 && x.now === 0 && x.avg === 0)).toBe(true)
  })
})

describe('earningsHero', () => {
  it('combines silver+loot+gather (never fame) with fame kept apart', () => {
    const h = earningsHero(summary)
    expect(h.total).toBe(1070) // netSilver, fame excluded
    expect(h.fame).toBe(5000)
    expect(h).toMatchObject({ now: 5000, avg: 6420, active: true, measuring: false, activeMs: 600000 })
  })

  it('flags measuring when rate not ready', () => {
    expect(earningsHero({ rateReady: false }).measuring).toBe(true)
  })
})
