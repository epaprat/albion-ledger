<script setup>
import { fmt, compact, tierLabel, qLabel, srcText } from '../format.js'
defineProps({ r: { type: Object, required: true } })
</script>

<template>
  <tr>
    <td :class="{ unknown: !r.item.known }">
      {{ r.item.displayName }}
      <span class="qty" v-if="r.count > 1">×{{ fmt(r.count) }}</span>
    </td>
    <td class="dim">{{ tierLabel(r.item) }}</td>
    <td class="dim">{{ qLabel(r.item.quality) }}</td>
    <td class="num">
      <span v-if="r.valuation.source === 'unknown'" class="muted">value unknown</span>
      <span v-else>{{ compact(r.valuation.amount) }}</span>
    </td>
    <td class="num">
      <span v-if="r.valuation.source === 'unknown'" class="muted">—</span>
      <span v-else class="stack">{{ compact(r.valuation.amount * r.count) }}</span>
    </td>
    <td><span class="badge" :class="r.valuation.source">{{ srcText[r.valuation.source] }}</span></td>
  </tr>
</template>

<style scoped>
tbody td, td { padding: 7px 16px; border-bottom: 1px solid var(--border); font-size: 14px; }
.num { text-align: right; font-variant-numeric: tabular-nums; }
.dim { color: var(--muted); }
.muted { color: var(--muted); }
.qty { color: var(--muted); font-variant-numeric: tabular-nums; margin-left: 6px; font-size: 12px; }
.stack { color: var(--accent); font-variant-numeric: tabular-nums; }
.unknown { color: var(--muted); font-style: italic; }
.badge { font-size: 11px; padding: 1px 6px; border-radius: 4px; }
.badge.live_market { background: rgba(63,185,80,.18); color: var(--good); }
.badge.server_estimate { background: rgba(210,153,34,.18); color: var(--warn); }
.badge.unknown { color: var(--muted); }
</style>
