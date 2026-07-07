<script setup>
import { ref, computed, onMounted } from 'vue'
import HoldingsPanel from './components/HoldingsPanel.vue'
import FlowPanel from './components/FlowPanel.vue'
import SessionSummaryBar from './components/SessionSummaryBar.vue'
import { fmt, compact, tierLabel, qLabel, srcText } from './format.js'
import { useTable } from './composables/useTable.js'
import StateBlock from './components/StateBlock.vue'
import SortTh from './components/SortTh.vue'
import FilterBar from './components/FilterBar.vue'

const tab = ref('flow')
const market = ref(new Map())        // index -> LiveViewItem
const holdings = ref([])             // HoldingItem[]
const summary = ref({ totalValue: 0, unvaluedCount: 0, cities: [] })
const flowEvents = ref([])           // FlowEventView[]
const flowGather = ref([])           // FlowItemStatView[] (gather breakdown)
const flowLoot = ref([])             // FlowItemStatView[] (loot breakdown)
const flowZones = ref([])            // ZoneStatView[] (006 — per-zone rates)
const flowView = ref('items')        // active FlowPanel view ('zones' gates the fetch)
const zoneWindow = ref('session')    // time window for zone stats
const flowSummary = ref({ active: false, netSilver: 0, silverPerHour: 0, lootValue: 0, gatherValue: 0, fame: 0, famePerHour: 0, rateReady: false, unvaluedCount: 0, eventCount: 0 })
const spec = ref({ masteries: [], nodeCount: 0, totalFame: 0, complete: false })
const exportResults = ref({})        // dataset key -> last ExportResult (013)
const exportBusy = ref(false)
const specFilter = ref('')
const specHideUntouched = ref(false)
const status = ref({ capturing: false, interface: '', encryptedRate: 0, driftAlert: '' })
const ready = ref(false)

const specOpen = ref({})              // "Category" | "Category/Sub" → collapsed?
const toggleSpec = (key) => { specOpen.value = { ...specOpen.value, [key]: !specOpen.value[key] } }
const specCollapsed = (key) => !!specOpen.value[key]
const pctMaxed = (g) => g.nodes > 0 ? Math.round((g.levelsum / (g.nodes * 100)) * 100) : 0
const levelsToGo = (g) => (g.nodes * 100) - g.levelsum

// Group the flat node list into category → subcategory → rows, each with a rollup
// (node count, summed fame, top level). Filter matches node/sub/category names.
const specTree = computed(() => {
  const q = specFilter.value.trim().toLowerCase()
  const rows = (spec.value.masteries || []).filter(m => {
    if (specHideUntouched.value && !m.touched) return false
    if (!q) return true
    return (m.name || '').toLowerCase().includes(q)
      || (m.subcategory || '').toLowerCase().includes(q)
      || (m.category || '').toLowerCase().includes(q)
  })
  const cats = new Map()
  for (const m of rows) {
    // Top group = gear SLOT (Weapon / Off-Hand / Head / Chest / Shoes for combat, or the
    // category for gathering/crafting/farming) so the tree reads like an equipment sheet.
    const cat = m.slot || m.category || 'Other'
    const sub = m.subcategory || '—'
    if (!cats.has(cat)) cats.set(cat, { name: cat, nodes: 0, touched: 0, fame: 0, levelsum: 0, maxed: 0, subs: new Map() })
    const c = cats.get(cat)
    if (!c.subs.has(sub)) c.subs.set(sub, { name: sub, nodes: 0, touched: 0, fame: 0, levelsum: 0, maxed: 0, rows: [] })
    const sc = c.subs.get(sub)
    const lvl = m.level || 0
    // Base "Fighter" aggregate → line header chip (structured flag, not a name suffix).
    if (m.base) { if (lvl > 0) sc.base = m; continue }
    // Clamp the rollup contribution to 100 — elite levels (100–120) must not push
    // "% maxed" past 100 or "lvls to go" negative.
    const capped = lvl > 100 ? 100 : lvl
    sc.rows.push(m); sc.nodes++; sc.fame += m.fame || 0; sc.levelsum += capped
    c.nodes++; c.fame += m.fame || 0; c.levelsum += capped
    if (m.touched) { sc.touched++; c.touched++ }
    if (lvl >= 100) { sc.maxed++; c.maxed++ }
  }
  // Fixed slot order (equipment sheet), then non-combat categories by fame.
  const order = ['Weapon', 'Off-Hand', 'Head', 'Chest', 'Shoes']
  let catList = [...cats.values()].sort((a, b) => {
    const ia = order.indexOf(a.name), ib = order.indexOf(b.name)
    if (ia !== -1 || ib !== -1) return (ia === -1 ? 99 : ia) - (ib === -1 ? 99 : ib)
    return b.fame - a.fame
  })
  for (const c of catList) {
    // Drop empty lines (path anchors — Trainee/Warrior/Hunter/Mage — carry no gear items).
    c.subList = [...c.subs.values()].filter(sc => sc.rows.length > 0).sort((a, b) => b.maxed - a.maxed || b.fame - a.fame)
    // maxed rows first, then by level desc, so what's done is at a glance.
    for (const sc of c.subList) sc.rows.sort((a, b) => (b.level - a.level) || (b.fame - a.fame))
    c.nodes = c.subList.reduce((n, sc) => n + sc.nodes, 0)
    c.maxed = c.subList.reduce((n, sc) => n + sc.maxed, 0)
  }
  catList = catList.filter(c => c.subList.length > 0)
  return catList
})
const marketList = computed(() => [...market.value.values()])
const marketAccessor = (r, k) => ({
  item: r.item.displayName, tier: (r.item.tier || 0) * 10 + (r.item.enchant || 0),
  quality: r.item.quality || 0, value: r.valuation.amount || 0, lastSeen: r.lastSeen || 0,
}[k])
const marketTable = useTable(marketList, {
  fields: [(r) => r.item.displayName, (r) => r.item.uniqueName],
  accessor: marketAccessor,
  defaultSort: { key: 'value', dir: 'desc' },
})
const encrypted = computed(() => (status.value.encryptedRate || 0) > 0.5)

const svc = () => (window.go && window.go.wailsadapter && window.go.wailsadapter.Service) || null

function upsertMarket(it) {
  if (!it || !it.item) return
  market.value.set(it.item.index, it)
  market.value = new Map(market.value)
}
async function refreshHoldings() {
  const s = svc(); if (!s) return
  holdings.value = (await s.ListHoldings()) || []
  summary.value = await s.HoldingsSummary()
}
// Coalesce holdings:changed bursts (a mass move fires ~2 events/item) into one
// refresh so the webview isn't rebuilt per event (Principle XI — bounded UI).
let refreshQueued = false
function scheduleHoldingsRefresh() {
  if (refreshQueued) return
  refreshQueued = true
  setTimeout(() => { refreshQueued = false; refreshHoldings() }, 80)
}

async function refreshFlow() {
  const s = svc(); if (!s) return
  flowEvents.value = (await s.ListFlow()) || []
  flowSummary.value = await s.FlowSummary()
  flowGather.value = (await s.FlowBreakdown('gather')) || []
  flowLoot.value = (await s.FlowBreakdown('loot')) || []
  // Zone stats hit the store — fetch only while the By zone view is actually visible.
  if (tab.value === 'flow' && flowView.value === 'zones') {
    flowZones.value = (await s.ZoneStats(zoneWindow.value)) || []
  }
}
function setFlowView(v) { flowView.value = v; if (v === 'zones') refreshFlow() }
function setZoneWindow(w) { zoneWindow.value = w; refreshFlow() }

// ── CSV export (013) ─────────────────────────────────────────────────────────
const exportSets = computed(() => ([
  { key: 'holdings', name: 'Holdings', rows: holdings.value.length },
  { key: 'flow', name: 'Activity Flow', rows: flowEvents.value.length },
  { key: 'zones', name: 'Zone Analytics', rows: flowZones.value.length },
  { key: 'market', name: 'Market Prices', rows: market.value.size },
  { key: 'spec', name: 'Destiny Board', rows: (spec.value.masteries || []).length },
]))
async function exportOne(key) {
  const s = svc(); if (!s || exportBusy.value) return
  exportBusy.value = true
  try {
    const res = await s.ExportDataset(key, zoneWindow.value)
    if (res && !res.canceled) exportResults.value = { ...exportResults.value, [key]: res }
  } finally { exportBusy.value = false }
}
async function exportAll() {
  const s = svc(); if (!s || exportBusy.value) return
  exportBusy.value = true
  try {
    const results = await s.ExportAll(zoneWindow.value)
    const next = { ...exportResults.value }
    for (const r of results || []) { if (!r.canceled) next[r.dataset] = r }
    exportResults.value = next
  } finally { exportBusy.value = false }
}
// Coalesce flow:changed bursts into one refresh (Principle XI — bounded UI).
let flowQueued = false
function scheduleFlowRefresh() {
  if (flowQueued) return
  flowQueued = true
  setTimeout(() => { flowQueued = false; refreshFlow() }, 80)
}

onMounted(async () => {
  const s = svc()
  if (!s) { ready.value = true; return }
  try {
    for (const it of (await s.ListItems()) || []) upsertMarket(it)
    await refreshHoldings()
    await refreshFlow()
    spec.value = await s.Spec()
    status.value = await s.Status()
  } catch (_) {}
  ready.value = true

  if (window.runtime) {
    window.runtime.EventsOn('item:updated', upsertMarket)
    window.runtime.EventsOn('status:changed', (st) => { status.value = st })
    window.runtime.EventsOn('drift:alert', (m) => { status.value = { ...status.value, driftAlert: m } })
    window.runtime.EventsOn('holdings:changed', scheduleHoldingsRefresh)
    window.runtime.EventsOn('flow:changed', scheduleFlowRefresh)
    window.runtime.EventsOn('spec:changed', (sp) => { spec.value = sp })
  }
})

</script>

<template>
  <div class="wrap">
    <header class="status" role="status" aria-live="polite">
      <span class="dot" :class="status.capturing ? 'on' : 'off'" aria-hidden="true"></span>
      <strong>{{ status.capturing ? 'Capturing' : 'Idle' }}</strong>
      <span class="muted" v-if="status.interface">· {{ status.interface }}</span>
      <span class="muted">· {{ fmt(status.decoded || 0) }} pkts</span>
      <span class="muted" v-if="status.gameServer">· {{ status.gameServer }}</span>
      <span class="muted">· encrypted {{ Math.round((status.encryptedRate || 0) * 100) }}%</span>
      <nav class="tabs" role="tablist" aria-label="Views">
        <button :class="{ active: tab === 'flow' }" @click="tab = 'flow'" role="tab" :aria-selected="tab === 'flow'">Flow</button>
        <button :class="{ active: tab === 'holdings' }" @click="tab = 'holdings'" role="tab" :aria-selected="tab === 'holdings'">Holdings</button>
        <button :class="{ active: tab === 'market' }" @click="tab = 'market'" role="tab" :aria-selected="tab === 'market'">Market</button>
        <button :class="{ active: tab === 'spec' }" @click="tab = 'spec'" role="tab" :aria-selected="tab === 'spec'">Spec</button>
        <button :class="{ active: tab === 'export' }" @click="tab = 'export'" role="tab" :aria-selected="tab === 'export'">Export</button>
      </nav>
    </header>

    <div class="drift" v-if="status.driftAlert" role="alert">⚠ {{ status.driftAlert }}</div>

    <main>
      <!-- FLOW (earnings) -->
      <template v-if="tab === 'flow'">
        <SessionSummaryBar :summary="flowSummary" />
        <FlowPanel
          :events="flowEvents" :gather="flowGather" :loot="flowLoot"
          :zones="flowZones" :zone-window="zoneWindow" :encrypted="encrypted"
          @update:view="setFlowView" @update:zone-window="setZoneWindow"
        />
      </template>

      <!-- HOLDINGS -->
      <HoldingsPanel
        v-else-if="tab === 'holdings'"
        :summary="summary"
        :holdings="holdings"
        :encrypted="encrypted"
      />

      <!-- MARKET -->
      <section v-else-if="tab === 'market'">
        <StateBlock v-if="marketList.length === 0" variant="empty" title="No market prices yet">
          Open the in-game marketplace and hover items — their prices land here.
        </StateBlock>
        <template v-else>
          <FilterBar v-model="marketTable.query.value" :shown="marketTable.shown.value" :total="marketTable.total.value" placeholder="Filter items…" />
          <StateBlock v-if="marketTable.shown.value === 0" variant="empty" title="No matches"
            :action="{ label: 'Clear filter', onClick: marketTable.clearFilter }">
            Nothing matches the current filter.
          </StateBlock>
          <table v-else>
            <thead><tr>
              <SortTh label="Item" col-key="item" :sort-key="marketTable.sortKey.value" :sort-dir="marketTable.sortDir.value" @toggle="k => marketTable.toggleSort(k, 'asc')" />
              <SortTh label="Tier" col-key="tier" :sort-key="marketTable.sortKey.value" :sort-dir="marketTable.sortDir.value" @toggle="marketTable.toggleSort" />
              <SortTh label="Quality" col-key="quality" :sort-key="marketTable.sortKey.value" :sort-dir="marketTable.sortDir.value" @toggle="marketTable.toggleSort" />
              <SortTh label="Value" col-key="value" align="num" :sort-key="marketTable.sortKey.value" :sort-dir="marketTable.sortDir.value" @toggle="marketTable.toggleSort" />
              <th>Source</th>
            </tr></thead>
            <tbody>
              <tr v-for="r in marketTable.visibleRows.value" :key="r.item.index">
                <td :class="{ unknown: !r.item.known }">{{ r.item.displayName }}</td>
                <td class="dim">{{ tierLabel(r.item) }}</td>
                <td class="dim">{{ qLabel(r.item.quality) }}</td>
                <td class="num">{{ fmt(r.valuation.amount) }}</td>
                <td><span class="badge" :class="r.valuation.source">{{ srcText[r.valuation.source] }}</span></td>
              </tr>
            </tbody>
          </table>
        </template>
      </section>

      <!-- EXPORT (CSV, 013) -->
      <section v-else-if="tab === 'export'" class="export-page">
        <div class="export-head">
          <div>
            <h2>Export</h2>
            <p class="muted">Save any dataset as an Excel-compatible CSV (UTF-8).</p>
          </div>
          <button class="export-all" :disabled="exportBusy" @click="exportAll">Export all…</button>
        </div>
        <table>
          <thead><tr><th>Dataset</th><th class="num">Rows</th><th>Last export</th><th></th></tr></thead>
          <tbody>
            <tr v-for="d in exportSets" :key="d.key">
              <td>{{ d.name }}</td>
              <td class="num" :class="{ dim: d.rows === 0 }">{{ d.rows === 0 ? '0 rows' : d.rows }}</td>
              <td class="export-status">
                <template v-if="exportResults[d.key]">
                  <span v-if="exportResults[d.key].err" class="export-err">{{ exportResults[d.key].err }}</span>
                  <span v-else class="export-ok">{{ exportResults[d.key].rows }} rows → {{ exportResults[d.key].path }}</span>
                </template>
                <span v-else class="dim">—</span>
              </td>
              <td class="num"><button :disabled="exportBusy" @click="exportOne(d.key)">Export…</button></td>
            </tr>
          </tbody>
        </table>
        <p class="muted export-note">Zone Analytics exports the currently selected time window ({{ zoneWindow }}).</p>
      </section>

      <!-- SPEC (Destiny Board, 011) -->
      <section v-else>
        <StateBlock v-if="!spec.masteries || spec.masteries.length === 0" variant="empty" title="Destiny Board not captured yet">
          Log out to character select and back in — your full skill tree streams in at login.
        </StateBlock>
        <template v-else>
          <div class="total" role="status" aria-live="polite">
            <span>Destiny Board</span>
            <strong>{{ spec.nodeCount }} nodes</strong>
            <span class="muted" v-if="spec.totalFame">· {{ compact(spec.totalFame) }} fame</span>
            <span class="filters">
              <label class="sr-pair" style="display:inline-flex;align-items:center;gap:5px;">
                <input v-model="specHideUntouched" type="checkbox" /> Only started
              </label>
              <label class="sr-pair">Filter
                <input v-model="specFilter" type="search" placeholder="node name…" aria-label="Filter nodes" />
              </label>
            </span>
          </div>
          <div v-if="!spec.complete" class="spec-note" role="note">
            The game never sends maxed (level-100) skills at login — they're learned from
            occasional broadcasts and remembered <strong>permanently</strong>. Long-idle maxed
            skills (no elite progress yet) may show as level 0 until their next progress
            tick; everything else is exact.
          </div>
          <StateBlock v-if="specTree.length === 0" variant="empty" title="No matching nodes"
            :action="{ label: 'Clear filter', onClick: () => { specFilter = '' } }">
            No skill nodes match the current filter.
          </StateBlock>
          <div v-else class="spec-tree">
            <div v-for="c in specTree" :key="c.name" class="spec-cat">
              <button class="spec-head cat" @click="toggleSpec(c.name)" :aria-expanded="!specCollapsed(c.name)">
                <span class="chev" :class="{ open: !specCollapsed(c.name) }">▸</span>
                <span class="spec-name">{{ c.name }}</span>
                <span class="slot-max" v-if="c.maxed">{{ c.maxed }} maxed</span>
                <span class="muted">· {{ c.nodes }} specs · {{ spec.complete ? '' : '≥' }}{{ pctMaxed(c) }}%</span>
              </button>
              <div v-show="!specCollapsed(c.name)" class="spec-subs">
                <div v-for="sc in c.subList" :key="sc.name" class="spec-sub">
                  <button class="spec-head sub" @click="toggleSpec(c.name + '/' + sc.name)" :aria-expanded="!specCollapsed(c.name + '/' + sc.name)">
                    <span class="chev" :class="{ open: !specCollapsed(c.name + '/' + sc.name) }">▸</span>
                    <span class="spec-name">{{ sc.name }}</span>
                    <span v-if="sc.base && sc.base.level > 0" class="fighter-lvl" :title="'Line fighter node level (from the in-progress snapshot; the game omits it once maxed)'">Fighter {{ sc.base.level }}</span>
                    <span class="muted">· {{ spec.complete ? '' : '≥' }}{{ pctMaxed(sc) }}% maxed · {{ sc.maxed }}/{{ sc.nodes }} at 100 · {{ fmt(levelsToGo(sc)) }} lvls to go</span>
                  </button>
                  <table v-show="!specCollapsed(c.name + '/' + sc.name)">
                    <caption class="sr-only">{{ c.name }} — {{ sc.name }}</caption>
                    <thead><tr><th></th><th class="num">Lvl</th><th>Progress</th><th class="num">Fame</th><th class="num">To 100</th></tr></thead>
                    <tbody>
                      <tr v-for="m in sc.rows" :key="m.index" :class="{ untouched: !m.touched, maxed: m.level >= 100 }">
                        <td>{{ m.name }}</td>
                        <td class="num">
                          <template v-if="m.level >= 100"><span class="lvl-max">{{ m.level }}</span><span class="elite-cap">/120</span></template>
                          <span v-else>{{ m.level }}</span>
                        </td>
                        <td>
                          <span class="bar" :class="{ elite: m.level >= 100 }" :title="m.level >= 100 ? ('Mastery maxed · elite ' + (m.level - 100) + '/20') : Math.round(m.progress * 100) + '% to next'">
                            <span class="bar-fill" :class="{ full: m.level >= 100 }" :style="{ width: (m.level >= 100 ? 100 : Math.round(m.progress * 100)) + '%' }"></span>
                            <span v-if="m.level >= 100" class="bar-elite" :style="{ width: Math.round(Math.min(m.level - 100, 20) / 20 * 100) + '%' }"></span>
                          </span>
                        </td>
                        <td class="num">{{ m.fame ? compact(m.fame) : '—' }}</td>
                        <td class="num">{{ m.level >= 100 ? '✓' : (100 - (m.level || 0)) + ' lvl' }}</td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          </div>
        </template>
      </section>
    </main>
  </div>
</template>

<style scoped>
.wrap { height: 100vh; display: flex; flex-direction: column; }
.status { display: flex; align-items: center; gap: 8px; padding: 10px 16px; font-size: 14px; background: var(--panel); border-bottom: 1px solid var(--border); }
.dot { width: 9px; height: 9px; border-radius: 50%; }
.dot.on { background: var(--good); box-shadow: 0 0 6px var(--good); } .dot.off { background: var(--muted); }
.muted { color: var(--muted); }
.tabs { margin-left: auto; display: flex; gap: 4px; }
.tabs button { background: transparent; border: 1px solid transparent; color: var(--muted); padding: 5px 14px; border-radius: 6px; cursor: pointer; font-size: 13px; box-shadow: none; }
.tabs button.active { background: var(--panel-2); color: var(--text); border-color: var(--border); }
.drift { padding: 8px 16px; background: #3a2d00; color: var(--warn); font-size: 13px; }
main { flex: 1; overflow: auto; }
.state { padding: 56px 24px; text-align: center; color: var(--muted); }
.state .big { font-size: 18px; color: var(--text); margin: 0 0 8px; }
table { width: 100%; border-collapse: collapse; font-size: 14px; }
thead th { position: sticky; top: 0; background: var(--panel); text-align: left; padding: 8px 16px; color: var(--muted); font-weight: 600; border-bottom: 1px solid var(--border); font-size: 12px; letter-spacing: .04em; text-transform: uppercase; }
tbody td { padding: 7px 16px; border-bottom: 1px solid var(--border); }
tbody tr:hover { background: var(--panel); }
.num { text-align: right; font-variant-numeric: tabular-nums; }
.dim { color: var(--muted); }
.unknown { color: var(--muted); font-style: italic; }
.badge { font-size: 11px; padding: 1px 6px; border-radius: 4px; }
.badge.live_market { background: rgba(63,185,80,.18); color: var(--good); }
.badge.server_estimate { background: rgba(210,153,34,.18); color: var(--warn); }
.badge.unknown { color: var(--muted); }
.badge.stale { background: rgba(248,81,73,.18); color: var(--bad); margin-left: 8px; }
.spec-note { margin: 8px 12px; padding: 10px 14px; background: var(--panel); border-left: 3px solid var(--warn); border-radius: 4px; font-size: 13px; color: var(--muted); line-height: 1.5; }
.spec-tree { display: flex; flex-direction: column; gap: 2px; }
.spec-head { display: flex; align-items: baseline; gap: 8px; width: 100%; text-align: left; background: none; border: none; color: var(--text); cursor: pointer; padding: 7px 10px; border-radius: 6px; font: inherit; }
.spec-head:hover { background: var(--panel); }
.spec-head.cat { font-weight: 600; font-size: 15px; }
.spec-head.sub { font-size: 13px; padding-left: 26px; color: var(--text); }
.spec-name { flex: 0 0 auto; }
.chev { display: inline-block; transition: transform .12s; font-size: 11px; color: var(--muted); }
.chev.open { transform: rotate(90deg); }
.spec-subs { display: flex; flex-direction: column; gap: 1px; }
.spec-sub table { margin-left: 40px; width: calc(100% - 40px); }
.untouched { opacity: 0.4; }
.export-page { max-width: 860px; }
.export-head { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 12px; }
.export-head h2 { margin: 0 0 2px; font-size: 18px; }
.export-all { font-weight: 600; }
.export-status { max-width: 380px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.export-ok { color: var(--good); }
.export-err { color: var(--bad, #e5534b); }
.export-note { margin-top: 10px; font-size: 12px; }
tr.maxed td { color: var(--good); }
tr.maxed td:first-child { font-weight: 600; }
.slot-max { font-size: 11px; font-weight: 600; padding: 1px 7px; border-radius: 10px; background: color-mix(in srgb, var(--good) 25%, transparent); color: var(--good); flex: 0 0 auto; }
.lvl-max { color: var(--good); font-weight: 700; }
.elite-cap { color: var(--muted); font-size: 11px; }
.bar.elite { display: inline-flex; }
.bar-elite { display: block; height: 100%; background: #d8a12a; }
.bar-fill.full { background: var(--good); opacity: 0.85; }
.fighter-lvl { font-size: 12px; padding: 1px 6px; border-radius: 4px; background: var(--border); color: var(--text); flex: 0 0 auto; }
.bar { display: inline-block; width: 120px; height: 8px; background: var(--border); border-radius: 4px; overflow: hidden; vertical-align: middle; }
.bar-fill { display: block; height: 100%; background: var(--good); }
</style>
