<script setup>
import { ref } from 'vue'
import { fmt, compact } from '../format.js'
defineProps({ r: { type: Object, required: true } })

const broken = ref(false) // some items (quest/unlock) have no render icon → fall back

const initials = (name) =>
  (name || '?').replace(/[^A-Za-z0-9 ]/g, '').split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]).join('').toUpperCase() || '?'

// Official item-render CDN (public icons). Quality tints the icon variant.
const iconUrl = (it) =>
  `https://render.albiononline.com/v1/item/${encodeURIComponent(it.uniqueName || 'UNKNOWN')}.png?quality=${it.quality || 1}&size=78`

const tierLabel = (it) => (it.tier ? `T${it.tier}${it.enchant ? '.' + it.enchant : ''}` : '')
const qLabel = (q) => (q ? ['', 'Normal', 'Good', 'Outstanding', 'Excellent', 'Masterpiece'][q] : 'Normal')
// Quality border colours, in-game style.
const qColor = (q) => ['#30363d', '#8b949e', '#3fb950', '#58a6ff', '#bc8cff', '#d4a017'][q || 1]
</script>

<template>
  <div class="tile">
    <div class="thumb" :style="{ borderColor: qColor(r.item.quality) }">
      <img v-if="!broken" :src="iconUrl(r.item)" :alt="r.item.displayName" loading="lazy" decoding="async" @error="broken = true" />
      <span v-else class="fallback" :aria-label="r.item.displayName">{{ initials(r.item.displayName) }}</span>
      <span class="qty" v-if="r.count > 1">×{{ fmt(r.count) }}</span>
    </div>
    <div class="vals">
      <span class="ea">{{ r.valuation.source === 'unknown' ? '—' : compact(r.valuation.amount) }}</span>
      <span class="tot" v-if="r.valuation.source !== 'unknown'">{{ compact(r.valuation.amount * r.count) }}</span>
    </div>
    <div class="tip" role="tooltip">
      <strong :class="{ unknown: !r.item.known }">{{ r.item.displayName }}</strong>
      <span class="meta">{{ [tierLabel(r.item), qLabel(r.item.quality)].filter(Boolean).join(' · ') }}</span>
      <span class="meta" v-if="r.count > 1">{{ fmt(r.count) }} × {{ compact(r.valuation.amount) }} = {{ compact(r.valuation.amount * r.count) }}</span>
    </div>
  </div>
</template>

<style scoped>
.tile { position: relative; width: 84px; display: flex; flex-direction: column; align-items: center; gap: 2px; padding: 4px; }
.thumb { position: relative; width: 64px; height: 64px; border: 2px solid #30363d; border-radius: 8px; background: #1c2128; display: grid; place-items: center; }
.thumb img { width: 60px; height: 60px; object-fit: contain; }
.fallback { font-size: 18px; font-weight: 700; color: var(--muted); letter-spacing: .5px; }
.qty { position: absolute; right: 2px; bottom: 1px; font-size: 11px; font-weight: 700; color: #fff; background: rgba(0,0,0,.7); border-radius: 4px; padding: 0 3px; font-variant-numeric: tabular-nums; }
.vals { display: flex; flex-direction: column; align-items: center; line-height: 1.2; }
.vals .ea { font-size: 11px; color: var(--muted); font-variant-numeric: tabular-nums; }
.vals .tot { font-size: 13px; color: var(--accent); font-variant-numeric: tabular-nums; font-weight: 600; }
/* Hover tooltip */
.tip { position: absolute; bottom: calc(100% - 2px); left: 50%; transform: translateX(-50%); z-index: 5;
  display: none; flex-direction: column; gap: 2px; white-space: nowrap; padding: 6px 9px; border-radius: 6px;
  background: var(--panel); border: 1px solid var(--border); box-shadow: 0 6px 18px rgba(0,0,0,.4); }
.tile:hover .tip { display: flex; }
.tip strong { font-size: 13px; }
.tip .unknown { color: var(--muted); font-style: italic; }
.tip .meta { font-size: 11px; color: var(--muted); font-variant-numeric: tabular-nums; }
</style>
