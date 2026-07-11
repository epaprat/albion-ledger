package holdings

// Ground-truth reconciliation (021): the game sends AUTHORITATIVE full snapshots that the
// client renders verbatim — op-2 key 55 is the whole bag, key 52 the whole equipped set,
// R:518/R:1 the whole bank tab. The app's DERIVED view must equal them; any divergence is
// an app-interpretation bug. These read-only helpers compare the wire truth to the app's
// view and report the difference by item, so the app flags its own mistakes automatically
// instead of a human eyeballing the game against the app (5 such bugs were found by hand in
// one session — this turns that loop into a log line, live OR under -replay).

import (
	"fmt"
	"sort"
	"strings"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

// ItemCount is one line of a container multiset: an item identity and its total stack count.
type ItemCount struct {
	Index, Quality, Count int
}

type msKey struct{ index, quality int }

// ReconcileResult is the outcome of comparing an authoritative wire snapshot to the app's
// derived view of the same container. Match is true when they agree exactly.
type ReconcileResult struct {
	Match   bool
	Extra   []ItemCount // app shows these but the wire doesn't — a leak/phantom/double-count
	Missing []ItemCount // the wire has these but the app dropped them — lost items
	Report  string      // human-readable one-liner (empty when Match)
}

// equippedTab is the tab label the pipeline gives the player's worn set (op-2 key 52); the
// bag (key 55) and any leaked foreign container use "Bag". Both are stored as LocInventory
// containers, so reconciliation separates the two SECTIONS the user sees by this label.
const equippedTab = "Equipped"

// ReconcileInventory compares the authoritative bag (op-2 key 55, resolved) to EVERY item
// the app shows in the BAG section — all inventory containers except the worn set, so a
// foreign container wrongly merged into the bag (a viewed loot bag) is caught here.
func (a *Aggregator) ReconcileInventory(wire []ItemCount) ReconcileResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reconcileLocked("BAG", wire, a.sectionMultisetLocked(func(c *container) bool {
		return c.location == model.LocInventory && c.tab != equippedTab
	}))
}

// ReconcileEquipped compares the authoritative worn set (op-2 key 52) to the app's Equipped
// section — the "Equipped"-tab inventory container plus the legacy LocEquipped slot.
func (a *Aggregator) ReconcileEquipped(wire []ItemCount) ReconcileResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	got := a.sectionMultisetLocked(func(c *container) bool {
		return c.location == model.LocInventory && c.tab == equippedTab
	})
	if a.equipped != nil {
		for _, it := range a.equipped.items {
			got[msKey{it.Item.Index, it.Item.Quality}] += stackCount(it)
		}
	}
	return a.reconcileLocked("EQUIPPED", wire, got)
}

// ReconcileBankTab compares the authoritative bank-tab content (R:518/R:1) to the app's
// DEDUPED view of that (city, tab) — physical wins over summary, i.e. exactly what the user
// sees — so a summary counted alongside its physical peer surfaces as Extra (double-count).
func (a *Aggregator) ReconcileBankTab(city, tab string, wire []ItemCount) ReconcileResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	physical := a.physicalBankTabsLocked()
	got := map[msKey]int{}
	for _, c := range a.containers {
		if c.location != model.LocBank || bankCityName(c.city) != bankCityName(city) || c.tab != tab {
			continue
		}
		if a.isDupSummaryLocked(c, physical) {
			continue
		}
		for _, it := range c.items {
			got[msKey{it.Item.Index, it.Item.Quality}] += stackCount(it)
		}
	}
	return a.reconcileLocked("BANK "+bankCityName(city)+"/"+tab, wire, got)
}

// sectionMultisetLocked sums (index,quality)->count over every container matching keep.
func (a *Aggregator) sectionMultisetLocked(keep func(*container) bool) map[msKey]int {
	m := map[msKey]int{}
	for _, c := range a.containers {
		if !keep(c) {
			continue
		}
		for _, it := range c.items {
			m[msKey{it.Item.Index, it.Item.Quality}] += stackCount(it)
		}
	}
	return m
}

// reconcileLocked diffs a wire multiset against a derived one and renders a report. The
// caller holds a.mu (item names resolve through the catalog).
func (a *Aggregator) reconcileLocked(label string, wire []ItemCount, got map[msKey]int) ReconcileResult {
	want := map[msKey]int{}
	for _, c := range wire {
		want[msKey{c.Index, c.Quality}] += c.Count
	}
	keys := map[msKey]struct{}{}
	for k := range want {
		keys[k] = struct{}{}
	}
	for k := range got {
		keys[k] = struct{}{}
	}
	var extra, missing []ItemCount
	for k := range keys {
		switch d := got[k] - want[k]; {
		case d > 0:
			extra = append(extra, ItemCount{k.index, k.quality, d})
		case d < 0:
			missing = append(missing, ItemCount{k.index, k.quality, -d})
		}
	}
	if len(extra) == 0 && len(missing) == 0 {
		return ReconcileResult{Match: true}
	}
	sortCounts(extra)
	sortCounts(missing)
	var b strings.Builder
	fmt.Fprintf(&b, "%s MISMATCH app≠wire (wire=%d app=%d)", label, sumCounts(want), sumCounts(got))
	if len(extra) > 0 {
		fmt.Fprintf(&b, " EXTRA[app has, wire doesn't]=%s", a.renderLocked(extra))
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, " MISSING[wire has, app dropped]=%s", a.renderLocked(missing))
	}
	return ReconcileResult{Extra: extra, Missing: missing, Report: b.String()}
}

// renderLocked formats item counts with readable names (caller holds a.mu).
func (a *Aggregator) renderLocked(items []ItemCount) string {
	parts := make([]string, len(items))
	for i, it := range items {
		name := a.cat.Resolve(it.Index, it.Quality).DisplayName
		if name == "" {
			name = fmt.Sprintf("idx%d", it.Index)
		}
		if it.Quality > 0 {
			parts[i] = fmt.Sprintf("%s(q%d)×%d", name, it.Quality, it.Count)
		} else {
			parts[i] = fmt.Sprintf("%s×%d", name, it.Count)
		}
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func stackCount(it model.HoldingItem) int {
	if it.Count < 1 {
		return 1
	}
	return it.Count
}

func sortCounts(cs []ItemCount) {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].Index != cs[j].Index {
			return cs[i].Index < cs[j].Index
		}
		return cs[i].Quality < cs[j].Quality
	})
}

func sumCounts(m map[msKey]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}
