<script setup>
import { computed } from 'vue'
import { compact } from '../format.js'

const props = defineProps({
  summary: { type: Object, required: true }, // SessionSummaryView
})

// Silver/hr shown only once the session has ≥1 min of activity (SC-006).
const silverRate = computed(() => props.summary.rateReady ? compact(props.summary.silverPerHour) + '/h' : 'measuring…')
const fameRate = computed(() => props.summary.rateReady ? compact(props.summary.famePerHour) + '/h' : 'measuring…')
</script>

<template>
  <div v-if="!summary.selfKnown" class="hint" role="status">
    ⓘ Change zones once (walk through a gate / portal) so we can identify your character — then
    silver, loot and gather start tracking. Fame already works.
  </div>
  <div class="bar" role="group" aria-label="Session earnings">
    <div class="cell primary">
      <span class="label">Net silver</span>
      <span class="value">{{ compact(summary.netSilver) }}</span>
      <span class="rate">{{ silverRate }}</span>
    </div>
    <div class="cell">
      <span class="label">Loot</span>
      <span class="value">{{ compact(summary.lootValue) }}</span>
    </div>
    <div class="cell">
      <span class="label">Gather</span>
      <span class="value">{{ compact(summary.gatherValue) }}</span>
    </div>
    <div class="cell fame">
      <span class="label">Fame</span>
      <span class="value">{{ compact(summary.fame) }}</span>
      <span class="rate">{{ fameRate }}</span>
    </div>
    <div class="cell state" :title="summary.active ? 'Session active' : 'Session idle'">
      <span class="dot" :class="summary.active ? 'on' : 'off'" aria-hidden="true"></span>
      <span class="label">{{ summary.active ? 'Active' : 'Idle' }}</span>
      <span class="rate" v-if="summary.unvaluedCount">{{ summary.unvaluedCount }} unvalued</span>
    </div>
  </div>
</template>

<style scoped>
.hint { padding: 8px 16px; background: rgba(88,166,255,.12); color: #58a6ff; font-size: 13px; border-bottom: 1px solid var(--border); }
.bar { display: flex; gap: 8px; padding: 12px 16px; background: var(--panel); border-bottom: 1px solid var(--border); flex-wrap: wrap; }
.cell { display: flex; flex-direction: column; min-width: 96px; padding: 4px 12px; border-radius: 8px; }
.cell.primary { background: var(--bg); border: 1px solid var(--border); }
.cell.fame { border-left: 2px solid var(--warn); }
.label { font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; }
.value { font-size: 20px; font-weight: 700; font-variant-numeric: tabular-nums; }
.rate { font-size: 12px; color: var(--muted); font-variant-numeric: tabular-nums; }
.cell.state { justify-content: center; }
.dot { width: 9px; height: 9px; border-radius: 50%; display: inline-block; }
.dot.on { background: var(--good); box-shadow: 0 0 6px var(--good); } .dot.off { background: var(--muted); }
</style>
