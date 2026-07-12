<script setup>
// SessionDashboard (022): the Flow thesis — "what have I earned this session, and how fast
// now vs. average?" A combined hero (silver-denominated session total + now/avg + fame kept
// apart) leads, then four stream cards (silver / loot / gather / fame), each showing total ·
// now/h · avg/h in one consistent vocabulary. Gold/accent is spent only on the hero total and
// the "now" rates (the live signal); cards stay neutral, fame visually separated (never
// silver, SC-005). Below the measuring threshold rates say "measuring…"; totals always show.
import { computed } from 'vue'
import { compact, dur } from '../format.js'
import { earningsCards, earningsHero } from '../composables/earnings.js'

const props = defineProps({
  summary: { type: Object, required: true }, // SessionSummary
})

const hero = computed(() => earningsHero(props.summary))
const cards = computed(() => earningsCards(props.summary))
const rate = (n) => '+' + compact(n) + '/h'
</script>

<template>
  <section class="dash" aria-label="Session earnings">
    <div v-if="!hero.selfKnown" class="hint" role="status">
      ⓘ Change zones once (walk through a gate / portal) so we can identify your character — then
      silver, loot and gather start tracking. Fame already works.
    </div>

    <!-- Combined session hero -->
    <div class="hero">
      <div class="hero-main">
        <p class="eyebrow">This session</p>
        <p class="total num">
          <span class="coin" aria-hidden="true">◈</span>{{ compact(hero.total) }}
          <span v-if="hero.fame" class="fame-inline num" title="Fame (separate from silver)">+{{ compact(hero.fame) }}<span class="u">fame</span></span>
        </p>
      </div>
      <div class="hero-rates" role="group" aria-label="Combined rates">
        <div class="hr">
          <span class="k">now</span>
          <span v-if="hero.measuring" class="v measuring">measuring<span class="dots" aria-hidden="true">…</span></span>
          <span v-else class="v now num">{{ rate(hero.now) }}</span>
        </div>
        <div class="hr">
          <span class="k">avg</span>
          <span v-if="hero.measuring" class="v measuring">—</span>
          <span v-else class="v num">{{ rate(hero.avg) }}</span>
        </div>
        <div class="hr">
          <span class="k">active</span>
          <span class="v num">{{ dur(hero.activeMs) }}</span>
        </div>
        <span class="live" :class="hero.active ? 'on' : 'off'">
          <span class="dot" aria-hidden="true"></span>{{ hero.active ? 'live' : 'idle' }}
        </span>
      </div>
    </div>

    <!-- Per-stream cards -->
    <div class="cards" role="group" aria-label="Per-stream earnings">
      <div v-for="c in cards" :key="c.key" class="card" :class="{ fame: c.isFame }">
        <p class="eyebrow">{{ c.label }}</p>
        <p class="c-total num">{{ compact(c.total) }}<span v-if="c.isFame" class="u"> fame</span></p>
        <div class="c-rates">
          <span class="c-r">
            <span class="k">now</span>
            <span v-if="hero.measuring" class="measuring">—</span>
            <span v-else class="now num">{{ rate(c.now) }}</span>
          </span>
          <span class="c-r">
            <span class="k">avg</span>
            <span v-if="hero.measuring" class="measuring">—</span>
            <span v-else class="num">{{ rate(c.avg) }}</span>
          </span>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.dash { background: var(--panel); border-bottom: 1px solid var(--border); }
.hint {
  padding: var(--space-2) var(--space-4);
  background: var(--panel-2);
  color: var(--muted);
  font-size: var(--text-md);
  border-bottom: 1px solid var(--border);
}

/* Hero */
.hero {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: var(--space-4);
  flex-wrap: wrap;
  padding: var(--space-4) var(--space-5) var(--space-3);
}
.eyebrow { margin: 0; font-size: 10px; color: var(--muted); text-transform: uppercase; letter-spacing: .08em; }
.total {
  margin: 2px 0 0;
  font-size: var(--text-hero);
  font-weight: 750;
  line-height: 1.02;
  letter-spacing: -.02em;
  color: var(--accent-bright);
}
.coin { font-size: .55em; margin-right: .14em; opacity: .8; vertical-align: .1em; }
.fame-inline { font-size: .34em; font-weight: 650; color: var(--muted); margin-left: .5em; vertical-align: .28em; letter-spacing: 0; }
.fame-inline .u { font-size: .8em; margin-left: 2px; }

.hero-rates { display: flex; align-items: center; gap: var(--space-4); flex-wrap: wrap; }
.hr { display: flex; flex-direction: column; gap: 1px; min-width: 60px; }
.hr .k { font-size: 10px; color: var(--muted); text-transform: uppercase; letter-spacing: .05em; }
.hr .v { font-size: var(--text-lg); font-weight: 700; }
.hr .v.now { color: var(--accent-bright); }
.hr .v.measuring { color: var(--muted); font-weight: 600; font-size: var(--text-md); }
.dots { animation: pulse 1.4s ease-in-out infinite; }
@keyframes pulse { 0%,100% { opacity: .3 } 50% { opacity: 1 } }
.live { display: inline-flex; align-items: center; gap: 6px; font-size: var(--text-sm); color: var(--muted); }
.live .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--muted); }
.live.on { color: var(--text); }
.live.on .dot { background: var(--good); box-shadow: 0 0 6px var(--good); }

/* Cards */
.cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--space-2);
  padding: 0 var(--space-5) var(--space-4);
}
.card {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius, 8px);
  padding: var(--space-3);
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.card.fame { border-left: 2px solid var(--muted); background: var(--panel-2); }
.c-total { margin: 0; font-size: var(--text-xl); font-weight: 750; letter-spacing: -.01em; }
.c-total .u { font-size: .5em; color: var(--muted); font-weight: 600; }
.c-rates { display: flex; gap: var(--space-3); margin-top: 2px; }
.c-r { display: flex; flex-direction: column; gap: 0; min-width: 0; }
.c-r .k { font-size: 9px; color: var(--muted); text-transform: uppercase; letter-spacing: .05em; }
.c-r .num { font-size: var(--text-md); font-weight: 650; }
.c-r .now { color: var(--accent-bright); }
.c-r .measuring { color: var(--muted); font-size: var(--text-md); }

@media (max-width: 720px) {
  .cards { grid-template-columns: repeat(2, 1fr); }
  .hero { align-items: flex-start; }
}
</style>
