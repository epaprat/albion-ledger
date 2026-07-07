<script setup>
// FilterBar (014): a labelled search input with a live shown/total count and a
// one-action clear (button or Escape). Shared by every data table (contract §4).
import { useId } from 'vue'
const fid = useId()
defineProps({
  modelValue: { type: String, default: '' },
  shown: { type: Number, default: 0 },
  total: { type: Number, default: 0 },
  placeholder: { type: String, default: 'Filter…' },
})
const emit = defineEmits(['update:modelValue'])
</script>

<template>
  <div class="filter-bar">
    <label class="sr-only" :for="fid">Filter</label>
    <input
      :id="fid"
      type="search"
      class="filter-input"
      :value="modelValue"
      :placeholder="placeholder"
      @input="emit('update:modelValue', $event.target.value)"
      @keydown.esc="emit('update:modelValue', '')"
    />
    <span class="filter-count num" aria-live="polite">
      {{ modelValue ? shown + ' / ' + total : total }}
    </span>
    <button v-if="modelValue" type="button" class="filter-clear" @click="emit('update:modelValue', '')">Clear</button>
  </div>
</template>

<style scoped>
.filter-bar { display: flex; align-items: center; gap: var(--space-3); margin-bottom: var(--space-3); }
.filter-input {
  flex: 1 1 260px;
  max-width: 320px;
  padding: var(--space-2) var(--space-3);
  font-size: var(--text-md);
  color: var(--text);
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 6px;
}
.filter-input::placeholder { color: var(--muted); }
.filter-count { font-size: var(--text-sm); color: var(--muted); }
.filter-clear {
  font-size: var(--text-sm);
  color: var(--muted);
  background: none;
  border: none;
  cursor: pointer;
  padding: 2px 6px;
}
.filter-clear:hover { color: var(--text); }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0; }
</style>
