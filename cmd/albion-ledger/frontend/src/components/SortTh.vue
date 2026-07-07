<script setup>
// SortTh (014): a sortable column header. Renders a real <button> inside a
// role-correct <th aria-sort> so the sort is keyboard-operable and announced to
// screen readers (contract §4/§5). The parent owns the sort state via useTable and
// reacts to @toggle.
defineProps({
  label: { type: String, required: true },
  colKey: { type: String, required: true },
  sortKey: { type: String, default: null },
  sortDir: { type: String, default: 'desc' },
  align: { type: String, default: 'left' }, // 'left' | 'num'
})
defineEmits(['toggle'])
</script>

<template>
  <th
    :class="align === 'num' ? 'num' : null"
    :aria-sort="sortKey === colKey ? (sortDir === 'asc' ? 'ascending' : 'descending') : 'none'"
    scope="col"
  >
    <button type="button" class="sort-btn" :class="{ active: sortKey === colKey }" @click="$emit('toggle', colKey)">
      <span>{{ label }}</span>
      <span class="arrow" aria-hidden="true">{{ sortKey === colKey ? (sortDir === 'asc' ? '▲' : '▼') : '' }}</span>
    </button>
  </th>
</template>

<style scoped>
.sort-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  width: 100%;
  padding: 0;
  font: inherit;
  color: var(--muted);
  background: none;
  border: none;
  cursor: pointer;
  text-transform: uppercase;
  letter-spacing: .04em;
  font-size: var(--text-sm);
}
th.num .sort-btn { justify-content: flex-end; }
.sort-btn:hover { color: var(--text); }
.sort-btn.active { color: var(--text); }
.arrow { font-size: 9px; color: var(--accent); }
</style>
