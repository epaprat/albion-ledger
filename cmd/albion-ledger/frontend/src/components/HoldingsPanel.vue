<script setup>
import { ref, computed, watch } from 'vue'
import HoldingRow from './HoldingRow.vue'
import HoldingTile from './HoldingTile.vue'
import { compact } from '../format.js'

const viewMode = ref('grid') // 'grid' | 'list'

const props = defineProps({
  summary: { type: Object, required: true }, // { totalValue, unvaluedCount, cities[] }
  holdings: { type: Array, required: true },  // HoldingItem[]
  encrypted: { type: Boolean, default: false },
  error: { type: String, default: '' },
})

const cityFilter = ref('all')
const tabFilter = ref('all')

const cities = computed(() => props.summary?.cities || [])

// Replicates the aggregator's city/tab grouping so item rows attach to their group.
const rowCity = (r) => (r.location === 'inventory' ? 'Inventory' : (r.city || 'Bank'))
const rowTab = (r) => r.group || 'Bank'

// NUL delimiter: cannot appear in a city/tab name, so composite keys never collide.
const SEP = '\u0000'
const itemsByKey = computed(() => {
  const m = {}
  for (const r of props.holdings) (m[rowCity(r) + SEP + rowTab(r)] ||= []).push(r)
  return m
})
const rowsFor = (cityName, tabName) => itemsByKey.value[cityName + SEP + tabName] || []

const cityNames = computed(() => cities.value.map((c) => c.name))
const visibleCities = computed(() =>
  cities.value.filter((c) => cityFilter.value === 'all' || c.name === cityFilter.value)
)
const tabsOf = (city) =>
  city.tabs.filter((t) => tabFilter.value === 'all' || t.name === tabFilter.value)

// Tab options follow the selected city: pick a city → only its tabs. "All" cities
// → the union across cities (deduped).
const tabOptions = computed(() => {
  if (cityFilter.value !== 'all') {
    const c = cities.value.find((x) => x.name === cityFilter.value)
    return c ? c.tabs.map((t) => t.name) : []
  }
  const s = new Set()
  for (const c of cities.value) for (const t of c.tabs) s.add(t.name)
  return [...s].sort()
})

// Changing the city resets the tab filter (a tab may not exist in the new city).
watch(cityFilter, () => { tabFilter.value = 'all' })

const staleLabel = (st) => {
  if (!st || !st.seen) return ''
  return st.stale ? 'stale' : ''
}
</script>

<template>
  <section aria-label="Holdings">
    <div class="total" role="status" aria-live="polite">
      <span>Holdings total</span>
      <strong>{{ compact(summary.totalValue) }}</strong>
      <span class="muted" v-if="summary.gameEstTotal" :title="'Sum of the game-reported vault estimates (K overview)'">· in-game est {{ compact(summary.gameEstTotal) }}</span>
      <span class="muted" v-if="summary.unvaluedCount">· {{ summary.unvaluedCount }} unvalued</span>
      <span class="filters" v-if="cities.length">
        <label class="sr-pair">City
          <select v-model="cityFilter" aria-label="Filter by city">
            <option value="all">All</option>
            <option v-for="n in cityNames" :key="n" :value="n">{{ n }}</option>
          </select>
        </label>
        <label class="sr-pair" v-if="tabOptions.length">Tab
          <select v-model="tabFilter" aria-label="Filter by tab">
            <option value="all">All</option>
            <option v-for="n in tabOptions" :key="n" :value="n">{{ n }}</option>
          </select>
        </label>
        <span class="toggle" role="group" aria-label="View mode">
          <button :class="{ active: viewMode === 'grid' }" @click="viewMode = 'grid'" :aria-pressed="viewMode === 'grid'">Grid</button>
          <button :class="{ active: viewMode === 'list' }" @click="viewMode = 'list'" :aria-pressed="viewMode === 'list'">List</button>
        </span>
      </span>
    </div>

    <!-- States -->
    <div v-if="error" class="state" role="alert">
      <p class="big">Something went wrong</p>
      <p class="muted">{{ error }}</p>
    </div>
    <div v-else-if="encrypted" class="state">
      <p class="big">Stream is encrypted</p>
      <p class="muted">The game traffic is encrypted right now, so holdings can't be read. This usually clears on its own.</p>
    </div>
    <div v-else-if="cities.length === 0" class="state">
      <p class="big">No holdings seen yet</p>
      <p class="muted">Open your inventory or a bank in game — held items appear here, valued.</p>
    </div>

    <!-- City → tab nesting -->
    <template v-else>
      <div v-for="city in visibleCities" :key="city.name" class="city">
        <div class="city-head">
          <h2>
            {{ city.name }}
            <span class="badge stale" v-if="staleLabel(city.state)">{{ staleLabel(city.state) }}</span>
          </h2>
          <span class="city-total num">
            {{ compact(city.total) }}
            <span class="muted" v-if="city.vaultValue" :title="'Game-reported vault total (K overview)'">· vault {{ compact(city.vaultValue) }}</span>
          </span>
        </div>

        <div v-for="t in tabsOf(city)" :key="city.name + '/' + t.name" class="group">
          <h3>
            {{ t.name }}
            <span class="muted">· {{ t.itemCount }}</span>
            <span class="muted" v-if="t.subtotal">· {{ compact(t.subtotal) }}</span>
            <span class="badge stale" v-if="staleLabel(t.state)">{{ staleLabel(t.state) }}</span>
          </h3>

          <p v-if="!t.opened" class="muted not-opened">Not opened yet — open this tab in game to see its contents.</p>
          <div v-else-if="viewMode === 'grid'" class="grid">
            <HoldingTile v-for="r in rowsFor(city.name, t.name)" :key="r.objId" :r="r" />
          </div>
          <table v-else>
            <caption class="sr-only">{{ city.name }} — {{ t.name }} ({{ t.itemCount }} items)</caption>
            <tbody>
              <HoldingRow v-for="r in rowsFor(city.name, t.name)" :key="r.objId" :r="r" />
            </tbody>
          </table>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.total { display: flex; align-items: baseline; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--border); }
.total strong { font-size: 18px; color: var(--accent); font-variant-numeric: tabular-nums; }
.muted { color: var(--muted); }
.filters { margin-left: auto; display: flex; gap: 12px; }
.filters select { background: var(--bg); color: var(--text); border: 1px solid var(--border); border-radius: 6px; padding: 3px 6px; font-size: 13px; }
.sr-pair { font-size: 12px; color: var(--muted); display: inline-flex; gap: 6px; align-items: center; }
.city { border-bottom: 1px solid var(--border); }
.city-head { display: flex; align-items: baseline; justify-content: space-between; padding: 12px 16px 4px; }
.city-head h2 { margin: 0; font-size: 15px; }
.city-total { color: var(--accent); font-variant-numeric: tabular-nums; }
.group { padding: 2px 0 10px; }
.group h3 { margin: 0; padding: 8px 16px 6px; font-size: 13px; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; }
/* Grid (in-game-bank style): icon tiles, value below, hover tooltip. */
.grid { display: flex; flex-wrap: wrap; gap: 4px; padding: 2px 12px 6px; }
/* View toggle */
.toggle { display: inline-flex; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
.toggle button { background: transparent; border: 0; color: var(--muted); padding: 3px 10px; font-size: 12px; cursor: pointer; }
.toggle button.active { background: var(--bg); color: var(--text); }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0,0,0,0); white-space: nowrap; border: 0; }
.filters select:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
.not-opened { padding: 2px 16px 8px; font-style: italic; }
.state { padding: 56px 24px; text-align: center; color: var(--muted); }
.state .big { font-size: 18px; color: var(--text); margin: 0 0 8px; }
table { width: 100%; border-collapse: collapse; }
.num { text-align: right; font-variant-numeric: tabular-nums; }
.badge { font-size: 11px; padding: 1px 6px; border-radius: 4px; }
.badge.stale { background: rgba(248,81,73,.18); color: var(--bad); margin-left: 8px; }
</style>
