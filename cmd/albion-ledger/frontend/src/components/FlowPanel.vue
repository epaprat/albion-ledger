<script setup>
import { ref, computed } from 'vue'
import { compact, fmt, tierLabel } from '../format.js'

const props = defineProps({
  events: { type: Array, default: () => [] },   // FlowEventView[]
  gather: { type: Array, default: () => [] },    // FlowItemStatView[] (gather breakdown)
  loot: { type: Array, default: () => [] },      // FlowItemStatView[] (loot breakdown)
  zones: { type: Array, default: () => [] },     // ZoneStatView[] (006 — per-zone rates)
  zoneWindow: { type: String, default: 'session' },
  encrypted: { type: Boolean, default: false },
})
const emit = defineEmits(['update:view', 'update:zoneWindow'])

// Three views: per-item breakdown, per-zone analytics (006), and the live stream.
const view = ref('items')
function setView(v) { view.value = v; emit('update:view', v) }

// ── By zone state ────────────────────────────────────────────────────────────
const WINDOWS = [
  { key: 'session', label: 'Session' },
  { key: 'today', label: 'Today' },
  { key: '7d', label: '7 days' },
  { key: 'all', label: 'All' },
]
const ZONE_METRICS = [
  { key: 'silverPerHour', label: 'Silver/h' },
  { key: 'gatherPerHour', label: 'Gather/h' },
  { key: 'famePerHour', label: 'Fame/h' },
]
const zoneSort = ref('silverPerHour')
const expandedZone = ref('')
const ZONE_CAP = 100 // top-N view; the full set stays in the payload (bounded UI)

const zoneRows = computed(() =>
  [...props.zones].sort((a, b) => (b[zoneSort.value] || 0) - (a[zoneSort.value] || 0)).slice(0, ZONE_CAP)
)
const activeMin = (ms) => {
  const m = Math.round((ms || 0) / 60000)
  return m >= 60 ? `${Math.floor(m / 60)}h ${m % 60}m` : `${m}m`
}
const toggleZone = (name) => { expandedZone.value = expandedZone.value === name ? '' : name }

const KINDS = ['all', 'silver', 'loot', 'gather', 'fame']
const filter = ref('all')

// Bounded render: the ledger keeps up to ~10k events; cap the DOM rows so the
// webview stays responsive (Principle XI — bounded UI).
const RENDER_CAP = 500

const filtered = computed(() => {
  const f = filter.value
  return f === 'all' ? props.events : props.events.filter(e => e.kind === f)
})
const rows = computed(() => filtered.value.slice(0, RENDER_CAP))
const hiddenCount = computed(() => Math.max(0, filtered.value.length - RENDER_CAP))

const gatherTotal = computed(() => props.gather.reduce((s, r) => s + (r.totalValue || 0), 0))
const lootTotal = computed(() => props.loot.reduce((s, r) => s + (r.totalValue || 0), 0))

const time = (ms) => new Date(ms || 0).toLocaleTimeString('en-US', { hour12: false })
const label = (e) => (e.kind === 'silver' ? 'Silver' : e.kind === 'fame' ? 'Fame' : (e.itemDisplayName || 'Unknown item'))
const amount = (e) => (e.kind === 'fame' ? compact(e.fame) + ' fame' : e.valued ? compact(e.silver) : 'unvalued')
</script>

<template>
  <section class="flow">
    <div class="viewtabs" role="tablist" aria-label="Flow view">
      <button :class="{ active: view === 'items' }" role="tab" :aria-selected="view === 'items'" @click="setView('items')">By item</button>
      <button :class="{ active: view === 'zones' }" role="tab" :aria-selected="view === 'zones'" @click="setView('zones')">By zone</button>
      <button :class="{ active: view === 'stream' }" role="tab" :aria-selected="view === 'stream'" @click="setView('stream')">Stream</button>
    </div>

    <!-- PER-ZONE ANALYTICS (006 — "where should I farm?") -->
    <template v-if="view === 'zones'">
      <div class="filters" role="tablist" aria-label="Time window">
        <button
          v-for="w in WINDOWS" :key="w.key"
          :class="{ active: zoneWindow === w.key }"
          role="tab" :aria-selected="zoneWindow === w.key"
          @click="emit('update:zoneWindow', w.key)"
        >{{ w.label }}</button>
        <span class="muted winlabel">rates over active time in: {{ WINDOWS.find(w => w.key === zoneWindow)?.label }}</span>
      </div>

      <div v-if="zones.length === 0" class="state">
        <p class="big">No zone data yet</p>
        <p class="muted" v-if="encrypted">The stream is currently encrypted — earnings can't be read right now.</p>
        <p class="muted" v-else>Play — every earning is stamped with its zone, and this table builds itself.</p>
      </div>

      <table v-else>
        <thead>
          <tr>
            <th>Zone</th>
            <th class="num">Active</th>
            <th
              v-for="m in ZONE_METRICS" :key="m.key"
              class="num sortable" :class="{ sorted: zoneSort === m.key }"
              role="button" tabindex="0" :aria-sort="zoneSort === m.key ? 'descending' : 'none'"
              @click="zoneSort = m.key" @keydown.enter="zoneSort = m.key"
            >{{ m.label }}{{ zoneSort === m.key ? ' ▾' : '' }}</th>
            <th class="num">Events</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="z in zoneRows" :key="z.zone">
            <tr
              class="zonerow" :class="{ insufficient: z.insufficientData }"
              role="button" tabindex="0" :aria-expanded="expandedZone === z.zone"
              @click="toggleZone(z.zone)" @keydown.enter="toggleZone(z.zone)"
            >
              <td>{{ expandedZone === z.zone ? '▾' : '▸' }} {{ z.zone }}</td>
              <td class="num dim">{{ activeMin(z.activeMs) }}</td>
              <td class="num">{{ z.insufficientData ? '—' : compact(z.silverPerHour) }}</td>
              <td class="num">{{ z.insufficientData ? '—' : compact(z.gatherPerHour) }}</td>
              <td class="num">{{ z.insufficientData ? '—' : compact(z.famePerHour) }}</td>
              <td class="num dim">{{ fmt(z.eventCount) }}</td>
            </tr>
            <tr v-if="z.insufficientData && expandedZone === z.zone" class="subrow">
              <td colspan="6" class="dim">Not enough active time (&lt;1 min) for reliable rates — totals: {{ compact(z.netSilver) }} silver, {{ compact(z.fame) }} fame.</td>
            </tr>
            <template v-else-if="expandedZone === z.zone">
              <tr v-for="a in z.activities" :key="z.zone + a.kind" class="subrow">
                <td class="sub"><span class="badge" :class="a.kind">{{ a.kind }}</span></td>
                <td class="num dim"></td>
                <td class="num" colspan="3">{{ compact(a.total) }}{{ a.kind === 'fame' ? ' fame' : '' }} · {{ compact(a.perHour) }}/h</td>
                <td class="num dim">{{ fmt(a.eventCount) }}</td>
              </tr>
            </template>
          </template>
        </tbody>
      </table>
    </template>

    <!-- PER-ITEM BREAKDOWN (AFM-style) -->
    <template v-else-if="view === 'items'">
      <div v-if="gather.length === 0 && loot.length === 0" class="state">
        <p class="big">Nothing gathered or looted yet</p>
        <p class="muted" v-if="encrypted">The stream is currently encrypted — can't read earnings right now.</p>
        <p class="muted" v-else>Gather a resource or loot a mob and the per-item breakdown builds here.</p>
      </div>

      <div v-else class="breakdowns">
        <div class="group" v-if="gather.length">
          <h3>Gather <span class="gtotal">{{ compact(gatherTotal) }}</span></h3>
          <table>
            <thead><tr><th>Item</th><th class="num">Qty</th><th class="num">EMV / item</th><th class="num">Stack EMV</th></tr></thead>
            <tbody>
              <tr v-for="r in gather" :key="'g' + r.uniqueName + r.quality">
                <td>{{ r.itemDisplayName }} <span class="dim" v-if="r.tier">· {{ tierLabel(r) }}</span></td>
                <td class="num">{{ fmt(r.qty) }}</td>
                <td class="num" :class="{ unvalued: !r.valued }">{{ r.valued ? compact(r.unitValue) : '—' }}</td>
                <td class="num" :class="{ unvalued: !r.valued }">{{ r.valued ? compact(r.totalValue) : 'unvalued' }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="group" v-if="loot.length">
          <h3>Loot <span class="gtotal">{{ compact(lootTotal) }}</span></h3>
          <table>
            <thead><tr><th>Item</th><th class="num">Qty</th><th class="num">EMV / item</th><th class="num">Stack EMV</th></tr></thead>
            <tbody>
              <tr v-for="r in loot" :key="'l' + r.uniqueName + r.quality">
                <td>{{ r.itemDisplayName }} <span class="dim" v-if="r.tier">· {{ tierLabel(r) }}</span></td>
                <td class="num">{{ fmt(r.qty) }}</td>
                <td class="num" :class="{ unvalued: !r.valued }">{{ r.valued ? compact(r.unitValue) : '—' }}</td>
                <td class="num" :class="{ unvalued: !r.valued }">{{ r.valued ? compact(r.totalValue) : 'unvalued' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>

    <!-- LIVE EVENT STREAM -->
    <template v-else>
      <div class="filters" role="tablist" aria-label="Flow type filter">
        <button
          v-for="k in KINDS" :key="k"
          :class="{ active: filter === k }"
          role="tab" :aria-selected="filter === k"
          @click="filter = k"
        >{{ k }}</button>
      </div>

      <div v-if="events.length === 0" class="state">
        <p class="big">No earnings yet</p>
        <p class="muted" v-if="encrypted">The stream is currently encrypted — earnings can't be read right now.</p>
        <p class="muted" v-else>Kill a mob, gather a resource, or earn fame and it shows up here live.</p>
      </div>

      <table v-else>
        <thead>
          <tr><th>Time</th><th>Type</th><th>Item</th><th>Zone</th><th class="num">Qty</th><th class="num">Value</th></tr>
        </thead>
        <tbody>
          <tr v-for="(e, i) in rows" :key="e.kind + e.ts + i">
            <td class="dim">{{ time(e.ts) }}</td>
            <td><span class="badge" :class="e.kind">{{ e.kind }}</span></td>
            <td>
              {{ label(e) }}
              <span class="dim" v-if="e.tier">· {{ tierLabel(e) }}</span>
            </td>
            <td class="dim">{{ e.zone || '—' }}</td>
            <td class="num">{{ e.count > 1 ? fmt(e.count) : '' }}</td>
            <td class="num" :class="{ unvalued: !e.valued && e.kind !== 'silver' && e.kind !== 'fame' }">{{ amount(e) }}</td>
          </tr>
        </tbody>
      </table>
      <p class="muted more" v-if="hiddenCount">+{{ fmt(hiddenCount) }} older not shown</p>
    </template>
  </section>
</template>

<style scoped>
.flow { display: flex; flex-direction: column; }
.viewtabs, .filters { display: flex; gap: 4px; padding: 10px 16px; }
.viewtabs button, .filters button { background: transparent; border: 1px solid transparent; color: var(--muted); padding: 4px 12px; border-radius: 6px; cursor: pointer; font-size: 13px; text-transform: capitalize; }
.viewtabs button.active, .filters button.active { background: var(--panel); color: var(--text); border-color: var(--border); }
.breakdowns { display: flex; flex-direction: column; gap: 20px; padding: 4px 0 20px; }
.group h3 { margin: 0; padding: 8px 16px; font-size: 14px; color: var(--text); display: flex; justify-content: space-between; }
.group h3 .gtotal { color: var(--good); font-variant-numeric: tabular-nums; }
.state { padding: 56px 24px; text-align: center; color: var(--muted); }
.state .big { font-size: 18px; color: var(--text); margin: 0 0 8px; }
table { width: 100%; border-collapse: collapse; font-size: 14px; }
thead th { position: sticky; top: 0; background: var(--panel); text-align: left; padding: 8px 16px; color: var(--muted); font-weight: 600; border-bottom: 1px solid var(--border); }
tbody td { padding: 7px 16px; border-bottom: 1px solid var(--border); }
tbody tr:hover { background: var(--panel); }
.num { text-align: right; font-variant-numeric: tabular-nums; }
.dim { color: var(--muted); }
.unvalued { color: var(--muted); font-style: italic; }
.more { text-align: center; padding: 8px; font-size: 12px; }
.winlabel { margin-left: auto; font-size: 12px; align-self: center; }
.sortable { cursor: pointer; user-select: none; }
.sortable.sorted { color: var(--text); }
.zonerow { cursor: pointer; }
.zonerow.insufficient td { color: var(--muted); font-style: italic; }
.subrow td { background: var(--panel); font-size: 13px; border-bottom: 1px solid var(--border); }
.subrow .sub { padding-left: 32px; }
.badge { font-size: 11px; padding: 1px 6px; border-radius: 4px; text-transform: capitalize; }
.badge.silver { background: rgba(210,153,34,.18); color: var(--warn); }
.badge.loot { background: rgba(63,185,80,.18); color: var(--good); }
.badge.gather { background: rgba(88,166,255,.18); color: #58a6ff; }
.badge.fame { background: rgba(188,140,255,.18); color: #bc8cff; }
</style>
