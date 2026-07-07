<script setup>
// SessionSummaryBar (014): leads with the HeroMetric (silver/hour thesis), then a
// quiet supporting row of loot / gather / fame. Fame stays visually separate — it is
// never silver (the metric separation carried from feature 005).
import { computed } from 'vue'
import { compact } from '../format.js'
import HeroMetric from './HeroMetric.vue'

const props = defineProps({
  summary: { type: Object, required: true },
})

const fameRate = computed(() => props.summary.rateReady ? compact(props.summary.famePerHour) + '/h' : 'measuring…')
</script>

<template>
  <div v-if="!summary.selfKnown" class="hint" role="status">
    ⓘ Change zones once (walk through a gate / portal) so we can identify your character — then
    silver, loot and gather start tracking. Fame already works.
  </div>
  <div class="bar">
    <HeroMetric :summary="summary" />
    <div class="support" role="group" aria-label="Session breakdown">
      <div class="stat">
        <span class="label">Loot</span>
        <span class="value num">{{ compact(summary.lootValue) }}</span>
      </div>
      <div class="stat">
        <span class="label">Gather</span>
        <span class="value num">{{ compact(summary.gatherValue) }}</span>
      </div>
      <div class="stat fame">
        <span class="label">Fame</span>
        <span class="value num">{{ compact(summary.fame) }}</span>
        <span class="sub num">{{ fameRate }}</span>
      </div>
      <div v-if="summary.unvaluedCount" class="stat">
        <span class="label">Unvalued</span>
        <span class="value num">{{ summary.unvaluedCount }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.hint {
  padding: var(--space-2) var(--space-4);
  background: color-mix(in srgb, var(--focus) 12%, transparent);
  color: var(--focus);
  font-size: var(--text-md);
  border-bottom: 1px solid var(--border);
}
.bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding-right: var(--space-5);
  background: var(--panel);
  border-bottom: 1px solid var(--border);
  flex-wrap: wrap;
}
.support { display: flex; gap: var(--space-5); padding: var(--space-3) 0; }
.stat { display: flex; flex-direction: column; gap: 2px; min-width: 72px; }
.stat.fame { padding-left: var(--space-4); border-left: 2px solid var(--warn); }
.label { font-size: 10px; color: var(--muted); text-transform: uppercase; letter-spacing: .06em; }
.value { font-size: var(--text-lg); font-weight: 700; }
.sub { font-size: var(--text-sm); color: var(--muted); }
</style>
