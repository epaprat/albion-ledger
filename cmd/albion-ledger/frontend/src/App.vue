<script setup>
import { ref, computed, onMounted } from 'vue'

const items = ref(new Map())   // index -> LiveViewItem
const status = ref({ capturing: false, interface: '', encryptedRate: 0, driftAlert: '' })
const ready = ref(false)

const rows = computed(() =>
  [...items.value.values()].sort((a, b) => (b.lastSeen || 0) - (a.lastSeen || 0))
)

function upsert(it) {
  if (!it || !it.item) return
  items.value.set(it.item.index, it)
  items.value = new Map(items.value) // trigger reactivity
}

const svc = () =>
  (window.go && window.go.wailsadapter && window.go.wailsadapter.Service) || null

onMounted(async () => {
  const s = svc()
  if (!s) { ready.value = true; return }   // running outside Wails (browser dev)
  try {
    const list = await s.ListItems()
    for (const it of list || []) upsert(it)
    status.value = await s.Status()
  } catch (_) {}
  ready.value = true

  if (window.runtime) {
    window.runtime.EventsOn('item:updated', upsert)
    window.runtime.EventsOn('status:changed', (st) => { status.value = st })
    window.runtime.EventsOn('drift:alert', (msg) => { status.value = { ...status.value, driftAlert: msg } })
  }
})

const fmtSilver = (n) => (n || 0).toLocaleString('en-US')
const tierLabel = (it) => it.tier ? `T${it.tier}${it.enchant ? '.' + it.enchant : ''}` : '—'
const qualityLabel = (q) => q ? ['', 'Normal', 'Good', 'Outstanding', 'Excellent', 'Masterpiece'][q] : '—'
const sourceLabel = { live_market: 'live', server_estimate: 'est', unknown: '—' }
</script>

<template>
  <div class="wrap">
    <header class="status" role="status" aria-live="polite">
      <span class="dot" :class="status.capturing ? 'on' : 'off'" aria-hidden="true"></span>
      <strong>{{ status.capturing ? 'Capturing' : 'Idle' }}</strong>
      <span class="muted" v-if="status.interface">· {{ status.interface }}</span>
      <span class="muted" v-if="status.gameServer">· {{ status.gameServer }}</span>
      <span class="muted">· encrypted {{ Math.round((status.encryptedRate || 0) * 100) }}%</span>
      <span class="spacer"></span>
      <span class="count">{{ rows.length }} items</span>
    </header>

    <div class="drift" v-if="status.driftAlert" role="alert">⚠ {{ status.driftAlert }}</div>

    <main>
      <div v-if="!ready" class="state">Loading…</div>

      <div v-else-if="rows.length === 0" class="state">
        <p class="big">No items captured yet</p>
        <p class="muted">Play the game — open the market, your bank, hover items. Captured items
          appear here by name with an estimated value.</p>
      </div>

      <table v-else>
        <thead>
          <tr>
            <th>Item</th><th>Tier</th><th>Quality</th><th class="num">Value</th><th>Source</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in rows" :key="r.item.index">
            <td :class="{ unknown: !r.item.known }">{{ r.item.displayName }}</td>
            <td>{{ tierLabel(r.item) }}</td>
            <td>{{ qualityLabel(r.item.quality) }}</td>
            <td class="num">{{ fmtSilver(r.valuation.amount) }}</td>
            <td>
              <span class="badge" :class="r.valuation.source">{{ sourceLabel[r.valuation.source] }}</span>
              <span v-if="r.valuation.stale" class="stale" title="value is stale">stale</span>
            </td>
          </tr>
        </tbody>
      </table>
    </main>
  </div>
</template>

<style scoped>
.wrap { height: 100vh; display: flex; flex-direction: column; }
.status {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 16px; background: var(--panel); border-bottom: 1px solid var(--border);
  font-size: 14px;
}
.dot { width: 9px; height: 9px; border-radius: 50%; }
.dot.on { background: var(--good); box-shadow: 0 0 6px var(--good); }
.dot.off { background: var(--muted); }
.muted { color: var(--muted); }
.spacer { flex: 1; }
.count { color: var(--muted); font-variant-numeric: tabular-nums; }
.drift { padding: 8px 16px; background: #3a2d00; color: var(--warn); border-bottom: 1px solid var(--border); font-size: 13px; }
main { flex: 1; overflow: auto; }
.state { padding: 64px 24px; text-align: center; color: var(--muted); }
.state .big { font-size: 18px; color: var(--text); margin: 0 0 8px; }
table { width: 100%; border-collapse: collapse; font-size: 14px; }
thead th {
  position: sticky; top: 0; background: var(--panel); text-align: left;
  padding: 10px 16px; color: var(--muted); font-weight: 600; border-bottom: 1px solid var(--border);
}
tbody td { padding: 8px 16px; border-bottom: 1px solid var(--border); }
tbody tr:hover { background: var(--panel); }
.num { text-align: right; font-variant-numeric: tabular-nums; }
.unknown { color: var(--muted); font-style: italic; }
.badge { font-size: 11px; padding: 1px 6px; border-radius: 4px; }
.badge.live_market { background: rgba(63,185,80,.18); color: var(--good); }
.badge.server_estimate { background: rgba(210,153,34,.18); color: var(--warn); }
.badge.unknown { color: var(--muted); }
.stale { margin-left: 6px; font-size: 11px; color: var(--bad); }
</style>
