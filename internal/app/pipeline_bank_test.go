package app

// K bank-overview pipeline goldens (feature 010) — contract rules 1-8 of
// specs/010-bank-overview/contracts/bank-overview.md, driven through the REAL
// dispatch path with wire-shaped params (kb.pcap layouts).

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func bankGUID(prefix byte) []byte {
	b := make([]byte, 16)
	b[0] = prefix
	for i := 1; i < 16; i++ {
		b[i] = byte(i)
	}
	return b
}

func locationsParams(clusters []string, prefixes []byte, values []int64) map[byte]interface{} {
	guids := []byte{}
	for _, p := range prefixes {
		guids = append(guids, bankGUID(p)...)
	}
	return map[byte]interface{}{
		1: guids, 2: clusters, 5: values, 253: int16(516),
	}
}

func tabsParams(vault byte, tabPrefixes []byte, names []string) map[byte]interface{} {
	guids := []byte{}
	for _, p := range tabPrefixes {
		guids = append(guids, bankGUID(p)...)
	}
	return map[byte]interface{}{
		0: bankGUID(vault), 1: guids, 2: names, 253: int16(517),
	}
}

func contentParams(tab byte, idx, counts []int16) map[byte]interface{} {
	// NOTE: no key 253 — mirrors the real R:1 content response (opByte fallback path).
	return map[byte]interface{}{
		0: bankGUID(tab), 1: int32(4), 2: idx, 4: counts,
	}
}

// Contract rules 1-3+7: full chain and the content-first ordering both converge.
func TestBankOverviewChain(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindResponse, 516, locationsParams([]string{"0006"}, []byte{0xAA}, []int64{110942100828}))
	p.dispatch(probe.KindResponse, 517, tabsParams(0xAA, []byte{0x11}, []string{"Hammadde"}))
	p.dispatch(probe.KindResponse, 1, contentParams(0x11, []int16{920, 837}, []int16{19, 2}))

	rows := svc.ListHoldings()
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r.Group != "Hammadde" {
			t.Fatalf("tab name must be the REAL name, got %q", r.Group)
		}
		if r.ObjID >= 0 {
			t.Fatalf("summary rows use synthetic negative ids, got %d", r.ObjID)
		}
	}
	// City vault total surfaced (raw ÷ 10000).
	sum := svc.HoldingsSummary()
	var vv int64
	for _, c := range sum.Cities {
		if !c.IsInventory {
			vv = c.VaultValue
		}
	}
	if vv != 11094210 {
		t.Fatalf("VaultValue = %d, want 11094210", vv)
	}
}

// Rule 2 (revised after live discovery — guids are EPHEMERAL per K opening):
// content without known tab meta is SKIPPED (a fallback identity could never be
// reconciled later); once meta arrives, content lands under the STABLE city+name
// identity, and repeated openings with fresh guids REPLACE instead of duplicating.
func TestBankContentBeforeMetaSkipsThenStableIdentity(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindResponse, 1, contentParams(0x11, []int16{920}, []int16{5}))
	if n := len(svc.ListHoldings()); n != 0 {
		t.Fatalf("meta-less content must be skipped, got %d rows", n)
	}
	// First opening.
	p.dispatch(probe.KindResponse, 516, locationsParams([]string{"0006"}, []byte{0xAA}, []int64{0}))
	p.dispatch(probe.KindResponse, 517, tabsParams(0xAA, []byte{0x11}, []string{"Setler"}))
	p.dispatch(probe.KindResponse, 1, contentParams(0x11, []int16{920}, []int16{5}))
	// Second opening: SAME tab, FRESH ephemeral guids (live 2026-07-05 behavior).
	p.dispatch(probe.KindResponse, 516, locationsParams([]string{"0006"}, []byte{0xCC}, []int64{0}))
	p.dispatch(probe.KindResponse, 517, tabsParams(0xCC, []byte{0x33}, []string{"Setler"}))
	p.dispatch(probe.KindResponse, 1, contentParams(0x33, []int16{920}, []int16{9}))
	rows := svc.ListHoldings()
	if len(rows) != 1 || rows[0].Group != "Setler" || rows[0].Count != 9 {
		t.Fatalf("re-opening must REPLACE under the stable identity, got %+v", rows)
	}
	// Old ephemeral guid's content is also refused now (meta was rebuilt).
	p.dispatch(probe.KindResponse, 1, contentParams(0x11, []int16{920}, []int16{99}))
	rows = svc.ListHoldings()
	if len(rows) != 1 || rows[0].Count != 9 {
		t.Fatalf("stale-guid content must not apply: %+v", rows)
	}
}

// Rule 4: an unrelated op-1 response (no key 253, wrong shape) must be ignored.
func TestBankContentShapeLock(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindResponse, 516, locationsParams([]string{"0006"}, []byte{0xAA}, []int64{0}))
	p.dispatch(probe.KindResponse, 517, tabsParams(0xAA, []byte{0x11}, []string{"Hammadde"}))
	p.dispatch(probe.KindResponse, 1, contentParams(0x11, []int16{920}, []int16{5}))
	before := len(svc.ListHoldings())

	// Hostile/unrelated op-1 shapes: int key0, short guid, mismatched arrays.
	p.dispatch(probe.KindResponse, 1, map[byte]interface{}{0: int64(12345), 2: []int16{1}, 4: []int16{1}})
	p.dispatch(probe.KindResponse, 1, map[byte]interface{}{0: bankGUID(0x11)[:8], 2: []int16{1}, 4: []int16{1}})
	p.dispatch(probe.KindResponse, 1, map[byte]interface{}{0: bankGUID(0x11), 2: []int16{1, 2}, 4: []int16{1}})
	if n := len(svc.ListHoldings()); n != before {
		t.Fatalf("hostile op-1 changed the view: %d → %d", before, n)
	}
}

// Rule 5 (revised — ephemeral guids killed guid-level merging): the K summary lives
// under its own stable identity; a physical open (99) is a separate source. The K
// tab itself never duplicates across openings (stable identity test above).
// Rule 6: synthetic rows never disturb the 008 move paths.
func TestBankSummaryAndMoveIsolation(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindResponse, 516, locationsParams([]string{"0006"}, []byte{0xAA}, []int64{0}))
	p.dispatch(probe.KindResponse, 517, tabsParams(0xAA, []byte{0x11}, []string{"Hammadde"}))
	p.dispatch(probe.KindResponse, 1, contentParams(0x11, []int16{920}, []int16{7}))
	if n := len(svc.ListHoldings()); n != 1 {
		t.Fatalf("setup: %d rows", n)
	}

	// A bag put + a physical bank container coexist without touching the K tab.
	p.registerNewItem(32, declParams(910, 920, 1))
	p.dispatch(probe.KindEvent, 26, putEvent(910, 2, tBagGUID))
	if !bagHas(svc, 920) {
		t.Fatal("008 put path broken by bank overview glue")
	}
	rows := svc.ListHoldings()
	if len(rows) != 2 {
		t.Fatalf("want K row + bag row, got %d", len(rows))
	}
	for _, r := range rows {
		if r.Group == "Hammadde" && r.ObjID >= 0 {
			t.Fatalf("K summary row lost its synthetic id: %+v", r)
		}
	}
}

// Rule 8: a second K opening REPLACES the vault totals (no accretion).
func TestBankSecondOpeningReplaces(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindResponse, 516, locationsParams([]string{"0006", "4001"}, []byte{0xAA, 0xBB}, []int64{50000, 70000}))
	p.dispatch(probe.KindResponse, 516, locationsParams([]string{"0006"}, []byte{0xAA}, []int64{80000}))
	sum := svc.HoldingsSummary()
	var n int
	for _, c := range sum.Cities {
		if c.VaultValue > 0 {
			n++
			if c.VaultValue != 8 {
				t.Fatalf("replaced value wrong: %d (want 8 = 80000/10000)", c.VaultValue)
			}
		}
	}
	if n != 1 {
		t.Fatalf("stale city total survived the replace: %d cities with values", n)
	}
}

// XI: tabMeta cap holds under a hostile flood of 517s; vaultCity rebuilds per 516.
func TestBankBridgesBounded(t *testing.T) {
	_, p := newGlue(t)
	for i := 0; i < 700; i++ {
		prefix := byte(i % 250)
		vault := byte(i % 100)
		params := tabsParams(vault, []byte{prefix}, []string{"X"})
		// vary the tab guid via the second byte too, so ids are unique
		g := params[1].([]byte)
		g[1] = byte(i / 250)
		p.dispatch(probe.KindResponse, 517, params)
	}
	if len(p.tabMeta) > 512 {
		t.Fatalf("tabMeta grew to %d (cap 512)", len(p.tabMeta))
	}
	for i := 0; i < 5; i++ {
		p.dispatch(probe.KindResponse, 516, locationsParams([]string{"0006"}, []byte{0xAA}, []int64{1}))
	}
	if len(p.vaultCity) != 1 {
		t.Fatalf("vaultCity must rebuild per 516, got %d", len(p.vaultCity))
	}
}
