import { ref, computed, unref, isRef } from 'vue'

// useTable (014): one sort + filter contract shared by every data table. The core
// is two pure functions (sortRows/filterRows) that the composable wires to reactive
// state. Sort/filter state is kept INDEPENDENT of the source list, so a live capture
// tick that swaps in a fresh array never resets the user's column or query (FR-005).

// sortRows returns a stably-sorted copy of rows by column key. key === null returns
// the source order untouched. accessor(row, key) yields the comparable value; string
// values compare case-insensitively, everything else by < / >.
export function sortRows(rows, key, dir, accessor) {
  if (!key) return rows.slice()
  const get = accessor || ((row, k) => row[k])
  const sign = dir === 'asc' ? 1 : -1
  return rows
    .map((row, i) => [row, i]) // index carries original order for a stable sort
    .sort((a, b) => {
      const cmp = compare(get(a[0], key), get(b[0], key))
      return cmp !== 0 ? cmp * sign : a[1] - b[1]
    })
    .map((pair) => pair[0])
}

function compare(a, b) {
  const an = a == null, bn = b == null
  if (an || bn) return an === bn ? 0 : an ? 1 : -1 // nulls sort last
  if (typeof a === 'string' || typeof b === 'string') {
    return String(a).toLowerCase().localeCompare(String(b).toLowerCase())
  }
  return a < b ? -1 : a > b ? 1 : 0
}

// filterRows keeps rows whose any listed field contains query (case-insensitive).
// Empty query keeps everything; field(row) yields the searchable string per field.
export function filterRows(rows, query, fields) {
  const q = (query || '').trim().toLowerCase()
  if (!q) return rows
  return rows.filter((row) => fields.some((f) => String(f(row) ?? '').toLowerCase().includes(q)))
}

// useTable wires the pure core to reactive view state. rowsRef is a ref/getter of the
// source rows; opts.fields is the array of field accessors for the text filter;
// opts.accessor maps a sort key to its comparable value; opts.defaultSort optionally
// primes { key, dir }.
export function useTable(rowsRef, opts = {}) {
  const { fields = [], accessor, defaultSort } = opts
  const sortKey = ref(defaultSort?.key ?? null)
  const sortDir = ref(defaultSort?.dir ?? 'desc')
  const query = ref('')

  const source = () => (typeof rowsRef === 'function' && !isRef(rowsRef) ? rowsRef() : unref(rowsRef)) || []
  const total = computed(() => source().length)
  const visibleRows = computed(() =>
    sortRows(filterRows(source(), query.value, fields), sortKey.value, sortDir.value, accessor)
  )
  const shown = computed(() => visibleRows.value.length)

  // toggleSort cycles a column: first click sorts (numeric desc / text asc by default),
  // a second click flips direction, a third clears back to the source order.
  function toggleSort(key, firstDir = 'desc') {
    if (sortKey.value !== key) {
      sortKey.value = key
      sortDir.value = firstDir
    } else if (sortDir.value === firstDir) {
      sortDir.value = firstDir === 'desc' ? 'asc' : 'desc'
    } else {
      sortKey.value = null
    }
  }

  function clearFilter() {
    query.value = ''
  }

  return { visibleRows, sortKey, sortDir, query, total, shown, toggleSort, clearFilter }
}
