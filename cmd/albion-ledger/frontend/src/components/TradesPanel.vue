<script setup>
import { computed, reactive, ref } from 'vue'
import { fmt, compact } from '../format.js'
import { useTable } from '../composables/useTable.js'
import { netClass as netClassOf, amountText as amountTextOf, sourceLabel, tradeFields, tradeAccessor, realizedWindows } from '../composables/trades.js'
import StateBlock from './StateBlock.vue'
import SortTh from './SortTh.vue'
import FilterBar from './FilterBar.vue'

const props = defineProps({
  trades: { type: Array, required: true },       // Trade[]
  summary: { type: Object, required: true },     // TradeSummary (for the selected window)
  window: { type: String, default: 'session' },  // realized-P&L window (018)
})
const emit = defineEmits(['update:window'])

// The ledger table is scoped to the SAME window as the hero, so the two never contradict
// (received >= windowStart; 0 = all time). Then the direction segment filters within it.
const windowed = computed(() => {
  const start = props.summary.windowStart || 0
  return start > 0 ? props.trades.filter((t) => (t.received || 0) >= start) : props.trades
})
const dirFilter = ref('all')
const filtered = computed(() =>
  dirFilter.value === 'all' ? windowed.value : windowed.value.filter((t) => t.direction === dirFilter.value))
const table = useTable(filtered, {
  fields: tradeFields,
  accessor: tradeAccessor,
  defaultSort: { key: 'received', dir: 'desc' },
})

const netClass = computed(() => netClassOf(props.summary.net))
const amountText = (r) => amountTextOf(r, fmt)
// Classify by P&L direction, not the transaction verb — a sell-order setup fee is an
// Expense, not a "Bought". Sold → Income, everything else (buy, fee) → Expense.
const typeLabel = (r) => (r.direction === 'sold' ? 'Income' : 'Expense')
const typeClass = (r) => (r.direction === 'sold' ? 'sold' : 'bought')
// When the trade happened — real sale time for mail order-fills, capture time for live.
// hourCycle:'h23' (not hour12:false) — WebKit renders midnight as "24:xx" with the latter.
const whenText = (ms) =>
  ms ? new Date(ms).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }) : '—'
// Reset BOTH the text query and the direction segment — an empty table can come from
// either, so the "Clear filter" action must clear both.
const clearFilters = () => { table.clearFilter(); dirFilter.value = 'all' }

// Item-render icon (official CDN, same as Holdings). A quicksell batch has no single
// item; broken/absent icons fall back to initials.
const broken = reactive({})
const iconUrl = (r) => `https://render.albiononline.com/v1/item/${encodeURIComponent(r.itemId)}.png?size=64`
const showIcon = (r) => r.source !== 'quicksell' && r.itemId && !broken[r.itemId]
const initials = (r) => {
  if (r.source === 'quicksell') return 'QS'
  const n = r.itemName || r.itemId || '?'
  return n.replace(/[^A-Za-z0-9 ]/g, '').split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]).join('').toUpperCase() || '?'
}
// Signed, coloured net for the last column.
const netText = (r) => (r.net >= 0 ? '+' : '') + fmt(r.net)
// realizedWindows drives the window selector; the label reads, the value hits the backend.
const windows = realizedWindows
</script>

<template>
  <section aria-label="Trades">
    <!-- Summary: realized P&L for the selected window; net leads, components broken out -->
    <div class="hero" role="status" aria-live="polite">
      <div class="net-box">
        <span class="net-label">Realized net · {{ summary.scope }}</span>
        <strong :class="netClass">{{ compact(summary.net) }}</strong>
      </div>
      <dl class="parts" v-if="summary.count">
        <div><dt>Income</dt><dd class="pos">{{ compact(summary.grossIncome) }}</dd></div>
        <div><dt>Sales tax</dt><dd class="fee">−{{ compact(summary.salesTax) }}</dd></div>
        <div v-if="summary.setupFee"><dt>Setup fees</dt><dd class="fee">−{{ compact(summary.setupFee) }}</dd></div>
        <div><dt>Expense</dt><dd class="neg">−{{ compact(summary.grossExpense) }}</dd></div>
      </dl>
      <span class="win seg" role="group" aria-label="Realized P&L window">
        <button v-for="w in windows" :key="w.value" :class="{ active: window === w.value }"
          @click="emit('update:window', w.value)" :aria-pressed="window === w.value">{{ w.label }}</button>
      </span>
      <span class="scope" :title="'Passive capture — order fills need the mail opened; instant/quicksell reconstructed from the wallet delta'">
        {{ summary.count }} trades
      </span>
    </div>

    <StateBlock v-if="windowed.length === 0" variant="empty"
      :title="trades.length ? 'No trades in this window' : 'No trades captured yet'">
      <template v-if="trades.length">Nothing in this time window — pick a wider window above to see more.</template>
      <template v-else>Sell or buy on the marketplace — order fills (open the mail), instant sells, quicksells and
      instant buys all land here, broken down by gross, tax and net. Passive, ToS-safe.</template>
    </StateBlock>

    <template v-else>
      <div class="controls">
        <FilterBar v-model="table.query.value" :shown="table.shown.value" :total="table.total.value" placeholder="Filter trades by item…" />
        <span class="seg" role="group" aria-label="Filter by direction">
          <button :class="{ active: dirFilter === 'all' }" @click="dirFilter = 'all'" :aria-pressed="dirFilter === 'all'">All</button>
          <button :class="{ active: dirFilter === 'sold' }" @click="dirFilter = 'sold'" :aria-pressed="dirFilter === 'sold'">Income</button>
          <button :class="{ active: dirFilter === 'bought' }" @click="dirFilter = 'bought'" :aria-pressed="dirFilter === 'bought'">Expense</button>
        </span>
      </div>
      <StateBlock v-if="table.shown.value === 0" variant="empty" title="No matches"
        :action="{ label: 'Clear filter', onClick: clearFilters }">
        Nothing matches the current filter.
      </StateBlock>
      <div v-else class="scroll">
        <table>
          <thead><tr>
            <th class="c-item">
              <SortTh label="Item" col-key="item" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="k => table.toggleSort(k, 'asc')" />
            </th>
            <SortTh label="When" col-key="received" align="num" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="table.toggleSort" />
            <SortTh label="Amount" col-key="amount" align="num" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="table.toggleSort" />
            <SortTh label="Gross" col-key="gross" align="num" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="table.toggleSort" />
            <SortTh label="Tax" col-key="tax" align="num" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="table.toggleSort" />
            <SortTh label="Setup" col-key="setup" align="num" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="table.toggleSort" />
            <SortTh label="Net" col-key="net" align="num" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="table.toggleSort" />
          </tr></thead>
          <tbody>
            <tr v-for="r in table.visibleRows.value" :key="r.tradeId">
              <td class="c-item">
                <span class="thumb" :class="r.direction">
                  <img v-if="showIcon(r)" :src="iconUrl(r)" :alt="r.itemName" loading="lazy" decoding="async" @error="broken[r.itemId] = true" />
                  <span v-else class="fallback">{{ initials(r) }}</span>
                </span>
                <span class="name-cell">
                  <span class="name">{{ r.itemName || r.itemId || 'Unknown item' }}</span>
                  <span class="tags">
                    <span class="badge" :class="typeClass(r)">{{ typeLabel(r) }}</span>
                    <span class="src" v-if="r.source !== 'mail'">{{ sourceLabel(r.source) }}</span>
                  </span>
                </span>
              </td>
              <td class="num dim when">{{ whenText(r.received) }}</td>
              <td class="num dim">{{ amountText(r) }}</td>
              <td class="num">{{ r.gross ? fmt(r.gross) : '—' }}</td>
              <td class="num fee">{{ r.salesTax ? '−' + fmt(r.salesTax) : '—' }}<span v-if="r.taxEstimated && r.salesTax" class="est" title="Estimated from the sales-tax rate">≈</span></td>
              <td class="num fee">{{ r.setupFee ? '−' + fmt(r.setupFee) : '—' }}</td>
              <td class="num net" :class="r.net >= 0 ? 'pos' : 'neg'">{{ netText(r) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </section>
</template>

<style scoped>
/* ── Hero summary ─────────────────────────────────────────────── */
.hero { display: flex; align-items: center; gap: 24px; flex-wrap: wrap; padding: 16px 20px; border-bottom: 1px solid var(--border); }
.net-box { display: flex; flex-direction: column; gap: 2px; }
.net-label { font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: .08em; }
.net-box strong { font-size: 26px; font-variant-numeric: tabular-nums; line-height: 1; }
.parts { display: flex; gap: 22px; margin: 0; }
.parts div { display: flex; flex-direction: column; gap: 2px; }
.parts dt { font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: .05em; }
.parts dd { margin: 0; font-size: 15px; font-variant-numeric: tabular-nums; }
.win { margin-left: auto; }
.scope { font-size: 12px; color: var(--muted); font-style: italic; }

.pos { color: var(--good, #3fb950); }
.neg { color: var(--bad, #f85149); }
.fee { color: var(--warn, #d29922); }

/* ── Table ────────────────────────────────────────────────────── */
.scroll { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; }
thead th { position: sticky; top: 0; background: var(--bg); z-index: 1; padding: 8px 14px; font-size: 11px; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); text-align: right; border-bottom: 1px solid var(--border); }
thead th.c-item { text-align: left; }
tbody td { padding: 8px 14px; text-align: right; border-bottom: 1px solid color-mix(in srgb, var(--border) 45%, transparent); font-variant-numeric: tabular-nums; }
tbody tr:hover td { background: color-mix(in srgb, var(--muted) 8%, transparent); }
.num { white-space: nowrap; }
.when { font-size: 12px; }
.dim { color: var(--muted); }
.net { font-weight: 600; }

/* Item cell: icon + name + tags */
.c-item { text-align: left; }
td.c-item { display: flex; align-items: center; gap: 12px; }
.thumb { flex: none; width: 40px; height: 40px; border-radius: 8px; border: 2px solid var(--border); background: #1c2128; display: grid; place-items: center; overflow: hidden; }
.thumb.sold { border-color: color-mix(in srgb, var(--good, #3fb950) 55%, var(--border)); }
.thumb.bought { border-color: color-mix(in srgb, var(--bad, #f85149) 55%, var(--border)); }
.thumb img { width: 38px; height: 38px; object-fit: contain; }
.fallback { font-size: 13px; font-weight: 700; color: var(--muted); letter-spacing: .5px; }
.name-cell { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.name { font-size: 14px; color: var(--text); }
.tags { display: flex; align-items: center; gap: 6px; }
.badge { font-size: 10px; font-weight: 600; padding: 1px 7px; border-radius: 4px; text-transform: uppercase; letter-spacing: .03em; }
.badge.sold { background: rgba(63,185,80,.16); color: var(--good, #3fb950); }
.badge.bought { background: rgba(248,81,73,.16); color: var(--bad, #f85149); }
.badge.fee { background: rgba(210,153,34,.16); color: var(--warn, #d29922); }

/* Controls row: item filter + direction segmented control */
.controls { display: flex; align-items: center; gap: 12px; }
.controls > :first-child { flex: 1; min-width: 0; }
.seg { display: inline-flex; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; flex: none; }
.seg button { background: transparent; border: 0; color: var(--muted); padding: 4px 12px; font-size: 12px; cursor: pointer; }
.seg button.active { background: var(--panel, #1c2128); color: var(--text); }
.seg button:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.src { font-size: 10px; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; }
.est { font-size: 10px; color: var(--muted); margin-left: 1px; }
</style>
