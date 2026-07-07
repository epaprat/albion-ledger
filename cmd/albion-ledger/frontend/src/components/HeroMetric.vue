<script setup>
// HeroMetric (014): the app's thesis — "how much am I earning right now?" The
// silver/hour rate is the single dominant number; net silver and session length
// support it. No fabricated zero: below the 1-minute measuring threshold it says
// "measuring…", and when the session goes idle it holds the last rate with an idle
// badge instead of a misleading live figure (contract §3, FR-001/FR-002).
import { computed } from 'vue'
import { compact, dur } from '../format.js'

const props = defineProps({
  summary: { type: Object, required: true }, // SessionSummary
})

const measuring = computed(() => !props.summary.rateReady)
const rate = computed(() => compact(props.summary.silverPerHour))
const netSign = computed(() => (props.summary.netSilver || 0) < 0 ? 'neg' : 'pos')
</script>

<template>
  <section class="hero" aria-label="Current earning rate" aria-live="polite">
    <p class="hero-eyebrow">Silver per hour</p>

    <p v-if="measuring" class="hero-rate measuring">measuring<span class="dots" aria-hidden="true">…</span></p>
    <p v-else class="hero-rate num">
      <span class="coin" aria-hidden="true">◈</span><span class="hero-sign" v-if="(summary.silverPerHour || 0) >= 0">+</span>{{ rate }}<span class="hero-unit">/h</span>
    </p>

    <div class="hero-support">
      <span class="chip" :class="netSign">
        <span class="chip-k">net</span>
        <span class="num">{{ (summary.netSilver || 0) >= 0 ? '+' : '' }}{{ compact(summary.netSilver) }}</span>
      </span>
      <span class="chip">
        <span class="chip-k">session</span>
        <span class="num">{{ dur(summary.elapsedMs) }}</span>
      </span>
      <span class="chip" :class="summary.active ? 'live' : 'idle'">
        <span class="dot" :class="summary.active ? 'on' : 'off'" aria-hidden="true"></span>
        {{ summary.active ? 'live' : 'idle' }}
      </span>
    </div>
  </section>
</template>

<style scoped>
.hero {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding: var(--space-4) var(--space-5);
  margin: var(--space-3);
  border-radius: 8px;
  background:
    radial-gradient(120% 140% at 0% 0%, rgba(201,162,74,.10), transparent 60%),
    linear-gradient(180deg, var(--panel-2), var(--panel));
  border: 1px solid var(--frame);
  box-shadow: inset 0 0 0 1px rgba(201,162,74,.18), inset 0 1px 0 rgba(255,255,255,.05), 0 2px 6px rgba(0,0,0,.4);
}
.hero-eyebrow {
  margin: 0;
  font-family: var(--font-display);
  font-size: var(--text-sm);
  color: var(--accent);
  text-transform: uppercase;
  letter-spacing: .14em;
}
.hero-rate {
  margin: 0;
  font-size: var(--text-hero);
  font-weight: 800;
  line-height: 1.05;
  letter-spacing: -.01em;
  color: var(--accent-bright);
  text-shadow: 0 0 18px rgba(232,199,102,.18), 0 1px 0 rgba(0,0,0,.5);
}
.coin { font-size: .62em; margin-right: .18em; opacity: .85; vertical-align: .06em; }
.hero-rate.measuring { color: var(--muted); font-weight: 600; font-size: var(--text-xl); }
.hero-sign { opacity: .55; }
.hero-unit { font-size: var(--text-lg); color: var(--muted); font-weight: 600; margin-left: 2px; }
.dots { animation: pulse 1.4s ease-in-out infinite; }
@keyframes pulse { 0%,100% { opacity: .3 } 50% { opacity: 1 } }

.hero-support { display: flex; gap: var(--space-2); flex-wrap: wrap; margin-top: var(--space-2); }
.chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 10px;
  border-radius: 999px;
  background: var(--bg);
  border: 1px solid var(--border);
  font-size: var(--text-sm);
  color: var(--text);
}
.chip-k { color: var(--muted); text-transform: uppercase; letter-spacing: .04em; font-size: 10px; }
.chip.pos .num { color: var(--good); }
.chip.neg .num { color: var(--bad); }
.chip.idle { color: var(--muted); }
.dot { width: 7px; height: 7px; border-radius: 50%; }
.dot.on { background: var(--good); box-shadow: 0 0 6px var(--good); }
.dot.off { background: var(--muted); }
</style>
