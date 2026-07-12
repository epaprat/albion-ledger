package holdings

import "testing"

// slotsOf builds resolved self-container slots from (index, count) pairs.
func slotsOf(objBase int, items ...[2]int) []SlotItem {
	out := make([]SlotItem, len(items))
	for i, it := range items {
		out[i] = SlotItem{ObjID: objBase + i, Ref: ItemRef{Index: it[0], Count: it[1]}}
	}
	return out
}

func counts(items ...[2]int) []ItemCount {
	out := make([]ItemCount, len(items))
	for i, it := range items {
		out[i] = ItemCount{Index: it[0], Count: it[1]}
	}
	return out
}

// 021: the authoritative bag (op-2) matching the app's bag view reconciles clean.
func TestReconcileInventoryMatch(t *testing.T) {
	a, _ := newAgg(t)
	a.SetSelfContainer("self-bag", "Bag", slotsOf(100, [2]int{920, 1}, [2]int{837, 3}), 1000)
	res := a.ReconcileInventory(counts([2]int{920, 1}, [2]int{837, 3}))
	if !res.Match {
		t.Fatalf("identical wire+app must reconcile clean, got %q", res.Report)
	}
}

// 021: a foreign container leaked into the bag (viewed mob loot) shows as EXTRA — the exact
// class of bug found by hand this session (bag jumped 4→11 while op-2 still said 4).
func TestReconcileInventoryDetectsLeak(t *testing.T) {
	a, _ := newAgg(t)
	a.SetSelfContainer("self-bag", "Bag", slotsOf(100, [2]int{920, 1}), 1000)
	// A loot bag wrongly ingested as inventory (unknown owner → LocInventory "Bag").
	a.SetContainer("lootbagguid", "notabank", slotsOf(200, [2]int{837, 2}), 1100)

	res := a.ReconcileInventory(counts([2]int{920, 1})) // op-2 truth = just item 920
	if res.Match {
		t.Fatal("a leaked loot container must NOT reconcile clean")
	}
	if len(res.Extra) != 1 || res.Extra[0].Index != 837 || res.Extra[0].Count != 2 {
		t.Fatalf("leak must surface as EXTRA idx837×2, got %+v (report %q)", res.Extra, res.Report)
	}
	if len(res.Missing) != 0 {
		t.Fatalf("nothing is missing here, got %+v", res.Missing)
	}
}

// 021: an item the wire has but the app lost (dropped loot) shows as MISSING.
func TestReconcileInventoryDetectsMissing(t *testing.T) {
	a, _ := newAgg(t)
	a.SetSelfContainer("self-bag", "Bag", slotsOf(100, [2]int{920, 1}), 1000)
	res := a.ReconcileInventory(counts([2]int{920, 1}, [2]int{837, 5})) // wire has 837, app doesn't
	if res.Match || len(res.Missing) != 1 || res.Missing[0].Index != 837 || res.Missing[0].Count != 5 {
		t.Fatalf("dropped item must surface as MISSING idx837×5, got %+v (report %q)", res.Missing, res.Report)
	}
}

// 021: the equipped section is separated from the bag by tab, so worn gear is NOT flagged as
// extra against the bag wire (and reconciles against its own key-52 wire).
func TestReconcileEquippedSeparateFromBag(t *testing.T) {
	a, _ := newAgg(t)
	a.SetSelfContainer("self-bag", "Bag", slotsOf(100, [2]int{920, 1}), 1000)
	a.SetSelfContainer("self-equipped", "Equipped", slotsOf(300, [2]int{837, 1}), 1000)

	if res := a.ReconcileInventory(counts([2]int{920, 1})); !res.Match {
		t.Fatalf("equipped must not leak into the bag reconcile, got %q", res.Report)
	}
	if res := a.ReconcileEquipped(counts([2]int{837, 1})); !res.Match {
		t.Fatalf("equipped must reconcile against its own wire, got %q", res.Report)
	}
}

// 021 (reconcile-caught): a bank tab that holds the SAME item+quality in several slots must
// COMBINE the counts, not keep only the last slot. R:518 sends one row per slot; the summary
// synthetic id is keyed on (index, quality), so overwriting silently dropped stacked resources
// (Thetford Hammadde short by 2997: Medium Hide 107 kept, 1998 lost).
func TestVaultSummaryCombinesStackedSlots(t *testing.T) {
	a, _ := newAgg(t)
	a.SetVaultSummaryTab("vault:Thetford:Hammadde", "Thetford", "Hammadde", []ItemRef{
		{Index: 920, Quality: 1, Count: 107},  // Medium Hide, slot A
		{Index: 920, Quality: 1, Count: 1998}, // Medium Hide, slot B — same item+quality
		{Index: 837, Quality: 1, Count: 999},  // a different item
	}, 1000)
	total := 0
	var hide int
	for _, r := range a.List() {
		total += r.Count
		if r.Item.Index == 920 {
			hide += r.Count
		}
	}
	if hide != 2105 {
		t.Fatalf("stacked slots must combine: Medium Hide want 2105 (107+1998), got %d", hide)
	}
	if total != 3104 {
		t.Fatalf("tab total want 3104 (2105+999), got %d", total)
	}
}

// 021 (reconcile-caught): a physically-opened bank tab shows stack counts from object
// declarations, which go stale when a stack changes; a fresher R:518 overview must patch the
// physical peer's counts (object ids preserved) so the total matches the game. FS tabs were
// short by a few stacks (Expert's Rune 58, etc.) until this refresh.
func TestVaultSummaryRefreshesStalePhysicalCounts(t *testing.T) {
	a, _ := newAgg(t)
	a.SetBankVault([]string{"o2"}, []string{"2"})
	a.SetCurrentCity("Fort Sterling")
	// Physical open: Expert's Rune (idx 837) at a STALE count of 100.
	a.SetContainer("phys2", "o2", []SlotItem{{ObjID: 6001, Ref: ItemRef{Index: 837, Quality: 1, Count: 100}}}, 1000)
	// Fresh overview says there are actually 158 (a 58 deposit landed after the declaration).
	a.SetVaultSummaryTab("vault:Fort Sterling:2", "Fort Sterling", "2", []ItemRef{{Index: 837, Quality: 1, Count: 158}}, 2000)

	rows := a.List()
	if len(rows) != 1 {
		t.Fatalf("still one deduped row, got %d: %+v", len(rows), rows)
	}
	if rows[0].ObjID != 6001 {
		t.Fatalf("physical object id must survive (move tracking), got %d", rows[0].ObjID)
	}
	if rows[0].Count != 158 {
		t.Fatalf("stale physical count must be refreshed to the fresh overview 158, got %d", rows[0].Count)
	}
}

// 021: a bank tab deduped correctly reconciles clean against R:518; a genuinely over-counted
// tab (two physical containers for the same city+tab) surfaces as EXTRA.
func TestReconcileBankTabDoubleCount(t *testing.T) {
	a, _ := newAgg(t)
	a.SetBankVault([]string{"o1"}, []string{"1"})
	a.SetCurrentCity("Fort Sterling")
	a.SetContainer("phys1", "o1", slotsOf(400, [2]int{920, 4}), 1000)
	// A summary for the SAME city+tab that the read-time dedup collapses — the view equals the
	// physical, so reconciling against the authoritative R:518 wire (4) is clean.
	a.SetVaultSummaryTab("vault:Fort Sterling:1", "Fort Sterling", "1", []ItemRef{{Index: 920, Count: 4}}, 900)
	if res := a.ReconcileBankTab("Fort Sterling", "1", counts([2]int{920, 4})); !res.Match {
		t.Fatalf("deduped tab must reconcile clean against R:518, got %q", res.Report)
	}
	// Inject a genuine over-count: a second physical container for the same tab (both count).
	a.SetContainer("phys1dup", "o1", slotsOf(500, [2]int{920, 4}), 1100)
	res := a.ReconcileBankTab("Fort Sterling", "1", counts([2]int{920, 4}))
	if res.Match || len(res.Extra) != 1 || res.Extra[0].Count != 4 {
		t.Fatalf("double-counted tab must surface EXTRA idx920×4, got %+v (report %q)", res.Extra, res.Report)
	}
}
