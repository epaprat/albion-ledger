<script setup>
// StateBlock (014): one designed treatment for every non-data screen state —
// empty / loading / stale / error / encrypted. Icon + title + explanation + an
// optional action, so no panel is ever blank and no raw error reaches the user
// (contract §2). Status-y variants announce themselves to screen readers.
import { computed } from 'vue'

const props = defineProps({
  variant: { type: String, default: 'empty' }, // empty|loading|stale|error|encrypted
  title: { type: String, required: true },
  action: { type: Object, default: null }, // { label, onClick }
})

const icon = computed(() => ({
  empty: '○',
  loading: '◍',
  stale: '◔',
  error: '△',
  encrypted: '⧉',
}[props.variant] || '○'))

// loading/stale/encrypted are live status; empty/error are static content.
const live = computed(() => ['loading', 'stale', 'encrypted'].includes(props.variant))
</script>

<template>
  <div class="state-block" :class="variant" :role="live ? 'status' : null" :aria-live="live ? 'polite' : null">
    <span class="sb-icon" :class="variant" aria-hidden="true">{{ icon }}</span>
    <p class="sb-title">{{ title }}</p>
    <p v-if="$slots.default" class="sb-desc"><slot /></p>
    <button v-if="action" class="sb-action" type="button" @click="action.onClick">{{ action.label }}</button>
  </div>
</template>

<style scoped>
.state-block {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-6) var(--space-4);
  text-align: center;
  min-height: 180px;
}
.sb-icon {
  font-size: var(--text-xl);
  line-height: 1;
  color: var(--muted);
}
.sb-icon.error { color: var(--bad); }
.sb-icon.encrypted, .sb-icon.stale { color: var(--warn); }
.sb-title {
  margin: 0;
  font-size: var(--text-lg);
  font-weight: 600;
  color: var(--text);
}
.sb-desc {
  margin: 0;
  max-width: 42ch;
  font-size: var(--text-md);
  color: var(--muted);
  line-height: 1.5;
}
.sb-action {
  margin-top: var(--space-2);
  padding: var(--space-2) var(--space-4);
  font-size: var(--text-md);
  font-weight: 600;
  color: var(--bg);
  background: var(--accent);
  border: none;
  border-radius: 6px;
  cursor: pointer;
}
.sb-action:hover { filter: brightness(1.08); }
</style>
