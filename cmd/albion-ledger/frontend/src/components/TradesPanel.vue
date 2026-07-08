<script setup>
import { computed, reactive, toRef } from 'vue'
import { fmt, compact } from '../format.js'
import { useTable } from '../composables/useTable.js'
import { dirLabel, netClass as netClassOf, amountText as amountTextOf, sourceLabel, tradeFields, tradeAccessor } from '../composables/trades.js'
import StateBlock from './StateBlock.vue'
import SortTh from './SortTh.vue'
import FilterBar from './FilterBar.vue'

const props = defineProps({
  trades: { type: Array, required: true },   // Trade[]
  summary: { type: Object, required: true }, // TradeSummary
})

const list = toRef(props, 'trades')
const table = useTable(list, {
  fields: tradeFields,
  accessor: tradeAccessor,
  defaultSort: { key: 'received', dir: 'desc' },
})

const netClass = computed(() => netClassOf(props.summary.net))
const amountText = (r) => amountTextOf(r, fmt)

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
</script>

<template>
  <section aria-label="Trades">
    <!-- Summary: net leads; income / tax / setup / expense broken out (FR-008) -->
    <div class="hero" role="status" aria-live="polite">
      <div class="net-box">
        <span class="net-label">Net trade</span>
        <strong :class="netClass">{{ compact(summary.net) }}</strong>
      </div>
      <dl class="parts">
        <div><dt>Income</dt><dd class="pos">{{ compact(summary.grossIncome) }}</dd></div>
        <div><dt>Sales tax</dt><dd class="fee">−{{ compact(summary.salesTax) }}</dd></div>
        <div v-if="summary.setupFee"><dt>Setup fees</dt><dd class="fee">−{{ compact(summary.setupFee) }}</dd></div>
        <div><dt>Expense</dt><dd class="neg">−{{ compact(summary.grossExpense) }}</dd></div>
      </dl>
      <span class="scope" :title="'Passive capture — order fills need the mail opened; instant/quicksell reconstructed from the wallet delta'">
        {{ summary.count }} trades · {{ summary.scope }}
      </span>
    </div>

    <StateBlock v-if="trades.length === 0" variant="empty" title="No trades captured yet">
      Sell or buy on the marketplace — order fills (open the mail), instant sells, quicksells and
      instant buys all land here, broken down by gross, tax and net. Passive, ToS-safe.
    </StateBlock>

    <template v-else>
      <FilterBar v-model="table.query.value" :shown="table.shown.value" :total="table.total.value" placeholder="Filter trades by item…" />
      <StateBlock v-if="table.shown.value === 0" variant="empty" title="No matches"
        :action="{ label: 'Clear filter', onClick: table.clearFilter }">
        Nothing matches the current filter.
      </StateBlock>
      <div v-else class="scroll">
        <table>
          <thead><tr>
            <th class="c-item">
              <SortTh label="Item" col-key="item" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="k => table.toggleSort(k, 'asc')" />
            </th>
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
                    <span class="badge" :class="r.direction">{{ dirLabel(r.direction) }}</span>
                    <span class="src" v-if="r.source !== 'mail'">{{ sourceLabel(r.source) }}</span>
                  </span>
                </span>
              </td>
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
.scope { margin-left: auto; font-size: 12px; color: var(--muted); font-style: italic; }

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
.src { font-size: 10px; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; }
.est { font-size: 10px; color: var(--muted); margin-left: 1px; }
</style>
