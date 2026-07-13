// Pure derivation for the Flow earnings dashboard (022). Kept out of the SFC so the
// per-stream card/hero shaping is unit-tested without a mount harness (mirrors trades.js).
//
// Vocabulary (consistent across the whole Flow screen): total (earned this session) ·
// now (smoothed EMA recent rate /h) · avg (session average /h). Four streams — silver,
// loot, gather, fame — with fame kept strictly separate from silver (SC-005).

// earningsCards maps a SessionSummary to the four stream cards, in display order.
// silver = silver-only (never loot/gather); fame carries isFame so the UI can separate it.
export function earningsCards(s) {
  s = s || {}
  return [
    { key: 'silver', label: 'Silver', total: s.silverValue || 0, now: s.silverNowPerHour || 0, avg: s.silverAvgPerHour || 0, isFame: false },
    { key: 'loot', label: 'Loot', total: s.lootValue || 0, now: s.lootNowPerHour || 0, avg: s.lootAvgPerHour || 0, isFame: false },
    { key: 'gather', label: 'Gather', total: s.gatherValue || 0, now: s.gatherNowPerHour || 0, avg: s.gatherAvgPerHour || 0, isFame: false },
    { key: 'fame', label: 'Fame', total: s.fame || 0, now: s.fameNowPerHour || 0, avg: s.famePerHour || 0, isFame: true },
  ]
}

// earningsHero maps a SessionSummary to the combined hero: silver-denominated session total
// (silver+loot+gather, never fame), combined now/avg rates, fame shown alongside but apart.
export function earningsHero(s) {
  s = s || {}
  return {
    total: s.netSilver || 0,
    fame: s.fame || 0,
    now: s.nowPerHour || 0,
    avg: s.silverPerHour || 0,
    activeMs: s.elapsedMs || 0,
    active: !!s.active,
    measuring: !s.rateReady,
    selfKnown: !!s.selfKnown,
  }
}
