<script setup>
import { ref, computed } from 'vue'
import { compact, fmt, tierLabel } from '../format.js'

const props = defineProps({
  events: { type: Array, default: () => [] },   // FlowEventView[]
  gather: { type: Array, default: () => [] },    // FlowItemStatView[] (gather breakdown)
  loot: { type: Array, default: () => [] },      // FlowItemStatView[] (loot breakdown)
  encrypted: { type: Boolean, default: false },
})

// Two views: the live event stream, and the AFM-style per-item breakdown.
const view = ref('items')

const KINDS = ['all', 'silver', 'loot', 'gather', 'fame']
const filter = ref('all')

// Bounded render: the ledger keeps up to ~10k events; cap the DOM rows so the
// webview stays responsive (Principle XI — bounded UI).
const RENDER_CAP = 500

const rows = computed(() => {
  const f = filter.value
  const src = f === 'all' ? props.events : props.events.filter(e => e.kind === f)
  return src.slice(0, RENDER_CAP)
})
const hiddenCount = computed(() => {
  const f = filter.value
  const total = f === 'all' ? props.events.length : props.events.filter(e => e.kind === f).length
  return Math.max(0, total - RENDER_CAP)
})

const gatherTotal = computed(() => props.gather.reduce((s, r) => s + (r.totalValue || 0), 0))
const lootTotal = computed(() => props.loot.reduce((s, r) => s + (r.totalValue || 0), 0))

const time = (ms) => new Date(ms || 0).toLocaleTimeString('en-US', { hour12: false })
const label = (e) => (e.kind === 'silver' ? 'Silver' : e.kind === 'fame' ? 'Fame' : (e.itemDisplayName || 'Unknown item'))
const amount = (e) => (e.kind === 'fame' ? compact(e.fame) + ' fame' : e.valued ? compact(e.silver) : 'unvalued')
</script>

<template>
  <section class="flow">
    <div class="viewtabs" role="tablist" aria-label="Flow view">
      <button :class="{ active: view === 'items' }" role="tab" :aria-selected="view === 'items'" @click="view = 'items'">By item</button>
      <button :class="{ active: view === 'stream' }" role="tab" :aria-selected="view === 'stream'" @click="view = 'stream'">Stream</button>
    </div>

    <!-- PER-ITEM BREAKDOWN (AFM-style) -->
    <template v-if="view === 'items'">
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
.badge { font-size: 11px; padding: 1px 6px; border-radius: 4px; text-transform: capitalize; }
.badge.silver { background: rgba(210,153,34,.18); color: var(--warn); }
.badge.loot { background: rgba(63,185,80,.18); color: var(--good); }
.badge.gather { background: rgba(88,166,255,.18); color: #58a6ff; }
.badge.fame { background: rgba(188,140,255,.18); color: #bc8cff; }
</style>
