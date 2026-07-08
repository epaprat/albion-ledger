<script setup>
import { computed, toRef } from 'vue'
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
// A signed silver value for the Net column (income +, expense −), coloured by sign.
const netCell = (r) => (r.net >= 0 ? '+' : '') + fmt(r.net)
</script>

<template>
  <section aria-label="Trades">
    <!-- Summary: net leads; income / tax / setup / expense broken out separately (FR-008) -->
    <div class="total" role="status" aria-live="polite">
      <span class="net" :title="'Net silver into your wallet from trades (income − expense)'">
        <span class="net-label">Net trade</span>
        <strong :class="netClass">{{ compact(summary.net) }}</strong>
      </span>
      <span class="sep">·</span>
      <span>Income <strong class="pos">{{ compact(summary.grossIncome) }}</strong></span>
      <span>Tax <strong class="tax">−{{ compact(summary.salesTax) }}</strong></span>
      <span v-if="summary.setupFee">Setup <strong class="tax">−{{ compact(summary.setupFee) }}</strong></span>
      <span>Expense <strong class="neg">−{{ compact(summary.grossExpense) }}</strong></span>
      <span class="muted" v-if="summary.count">· {{ summary.count }} trades</span>
      <span class="muted scope" :title="'Passive capture — mails must be opened; instant/quicksell reconstructed from wallet delta'">· {{ summary.scope }}</span>
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
            <SortTh label="Type" col-key="direction" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="k => table.toggleSort(k, 'asc')" />
            <SortTh label="Item" col-key="item" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="k => table.toggleSort(k, 'asc')" />
            <SortTh label="Amount" col-key="amount" align="num" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="table.toggleSort" />
            <SortTh label="Gross" col-key="gross" align="num" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="table.toggleSort" />
            <SortTh label="Tax" col-key="tax" align="num" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="table.toggleSort" />
            <SortTh label="Setup" col-key="setup" align="num" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="table.toggleSort" />
            <SortTh label="Net" col-key="net" align="num" :sort-key="table.sortKey.value" :sort-dir="table.sortDir.value" @toggle="table.toggleSort" />
          </tr></thead>
          <tbody>
            <tr v-for="r in table.visibleRows.value" :key="r.tradeId">
              <td>
                <span class="badge" :class="r.direction">{{ dirLabel(r.direction) }}</span>
                <span class="src" v-if="r.source !== 'mail'">{{ sourceLabel(r.source) }}</span>
              </td>
              <td>{{ r.itemName || r.itemId || 'Unknown item' }}</td>
              <td class="num dim">{{ amountText(r) }}</td>
              <td class="num dim">{{ fmt(r.gross) }}</td>
              <td class="num tax">{{ r.salesTax ? '−' + fmt(r.salesTax) : '—' }}<span v-if="r.taxEstimated && r.salesTax" class="est" title="Estimated from the sales-tax rate (no wallet delta observed)">≈</span></td>
              <td class="num tax">{{ r.setupFee ? '−' + fmt(r.setupFee) : '—' }}</td>
              <td class="num" :class="r.net >= 0 ? 'pos' : 'neg'">{{ netCell(r) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </section>
</template>

<style scoped>
.total { display: flex; align-items: baseline; gap: 12px; padding: 12px 16px; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
.total strong { font-variant-numeric: tabular-nums; }
.net { display: inline-flex; align-items: baseline; gap: 6px; }
.net-label { font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: .06em; }
.net strong { font-size: 20px; }
.sep { color: var(--border); }
.muted { color: var(--muted); }
.scope { font-style: italic; }
.pos { color: var(--good, #3fb950); }
.neg { color: var(--bad, #f85149); }
.tax { color: var(--warn, #d29922); }
.scroll { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; }
.num { text-align: right; font-variant-numeric: tabular-nums; }
.dim { color: var(--muted); }
.src { font-size: 10px; color: var(--muted); margin-left: 6px; text-transform: uppercase; letter-spacing: .04em; }
.est { font-size: 10px; color: var(--muted); margin-left: 1px; }
.badge { font-size: 11px; padding: 1px 8px; border-radius: 4px; }
.badge.sold { background: rgba(63,185,80,.18); color: var(--good, #3fb950); }
.badge.bought { background: rgba(248,81,73,.18); color: var(--bad, #f85149); }
</style>
