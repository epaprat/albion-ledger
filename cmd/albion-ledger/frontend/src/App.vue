<script setup>
import { ref, computed, onMounted } from 'vue'
import HoldingsPanel from './components/HoldingsPanel.vue'
import { fmt, tierLabel, qLabel, srcText } from './format.js'

const tab = ref('holdings')
const market = ref(new Map())        // index -> LiveViewItem
const holdings = ref([])             // HoldingItem[]
const summary = ref({ totalValue: 0, unvaluedCount: 0, cities: [] })
const spec = ref({ masteries: [] })
const status = ref({ capturing: false, interface: '', encryptedRate: 0, driftAlert: '' })
const ready = ref(false)

const marketRows = computed(() =>
  [...market.value.values()].sort((a, b) => (b.lastSeen || 0) - (a.lastSeen || 0))
)
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

onMounted(async () => {
  const s = svc()
  if (!s) { ready.value = true; return }
  try {
    for (const it of (await s.ListItems()) || []) upsertMarket(it)
    await refreshHoldings()
    spec.value = await s.Spec()
    status.value = await s.Status()
  } catch (_) {}
  ready.value = true

  if (window.runtime) {
    window.runtime.EventsOn('item:updated', upsertMarket)
    window.runtime.EventsOn('status:changed', (st) => { status.value = st })
    window.runtime.EventsOn('drift:alert', (m) => { status.value = { ...status.value, driftAlert: m } })
    window.runtime.EventsOn('holdings:changed', scheduleHoldingsRefresh)
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
        <button :class="{ active: tab === 'holdings' }" @click="tab = 'holdings'" role="tab" :aria-selected="tab === 'holdings'">Holdings</button>
        <button :class="{ active: tab === 'market' }" @click="tab = 'market'" role="tab" :aria-selected="tab === 'market'">Market</button>
        <button :class="{ active: tab === 'spec' }" @click="tab = 'spec'" role="tab" :aria-selected="tab === 'spec'">Spec</button>
      </nav>
    </header>

    <div class="drift" v-if="status.driftAlert" role="alert">⚠ {{ status.driftAlert }}</div>

    <main>
      <!-- HOLDINGS -->
      <HoldingsPanel
        v-if="tab === 'holdings'"
        :summary="summary"
        :holdings="holdings"
        :encrypted="encrypted"
      />

      <!-- MARKET -->
      <section v-else-if="tab === 'market'">
        <div v-if="marketRows.length === 0" class="state">
          <p class="big">No market items yet</p>
          <p class="muted">Open the marketplace and click items.</p>
        </div>
        <table v-else>
          <thead><tr><th>Item</th><th>Tier</th><th>Quality</th><th class="num">Value</th><th>Source</th></tr></thead>
          <tbody>
            <tr v-for="r in marketRows" :key="r.item.index">
              <td :class="{ unknown: !r.item.known }">{{ r.item.displayName }}</td>
              <td class="dim">{{ tierLabel(r.item) }}</td>
              <td class="dim">{{ qLabel(r.item.quality) }}</td>
              <td class="num">{{ fmt(r.valuation.amount) }}</td>
              <td><span class="badge" :class="r.valuation.source">{{ srcText[r.valuation.source] }}</span></td>
            </tr>
          </tbody>
        </table>
      </section>

      <!-- SPEC -->
      <section v-else>
        <div v-if="!spec.masteries || spec.masteries.length === 0" class="state">
          <p class="big">No specs captured yet</p>
          <p class="muted">Open your character / destiny board (or relog) so the data streams.</p>
        </div>
        <table v-else>
          <thead><tr><th>Mastery</th><th class="num">Level</th></tr></thead>
          <tbody>
            <tr v-for="m in spec.masteries.filter(x => x.level > 0)" :key="m.index">
              <td>{{ m.name }}</td><td class="num">{{ m.level }}</td>
            </tr>
          </tbody>
        </table>
      </section>
    </main>
  </div>
</template>

<style scoped>
.wrap { height: 100vh; display: flex; flex-direction: column; }
.status { display: flex; align-items: center; gap: 8px; padding: 10px 16px; background: var(--panel); border-bottom: 1px solid var(--border); font-size: 14px; }
.dot { width: 9px; height: 9px; border-radius: 50%; }
.dot.on { background: var(--good); box-shadow: 0 0 6px var(--good); } .dot.off { background: var(--muted); }
.muted { color: var(--muted); }
.tabs { margin-left: auto; display: flex; gap: 4px; }
.tabs button { background: transparent; border: 1px solid transparent; color: var(--muted); padding: 4px 12px; border-radius: 6px; cursor: pointer; font-size: 13px; }
.tabs button.active { background: var(--bg); color: var(--text); border-color: var(--border); }
.drift { padding: 8px 16px; background: #3a2d00; color: var(--warn); font-size: 13px; }
main { flex: 1; overflow: auto; }
.state { padding: 56px 24px; text-align: center; color: var(--muted); }
.state .big { font-size: 18px; color: var(--text); margin: 0 0 8px; }
table { width: 100%; border-collapse: collapse; font-size: 14px; }
thead th { position: sticky; top: 0; background: var(--panel); text-align: left; padding: 8px 16px; color: var(--muted); font-weight: 600; border-bottom: 1px solid var(--border); }
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
</style>
