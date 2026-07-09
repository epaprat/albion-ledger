package app

// SC-002 proof (009): a new category handler is added with ONLY a register call —
// zero lines change in dispatch.go — and the dispatcher routes to it. Plus the
// duplicate-registration guard (a silent shadow would be a startup-order bug).

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/epaprat/albion-ledger/data"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func TestRegistryAcceptsNewCategoryWithoutCentralChanges(t *testing.T) {
	// A category the real codes.json never produces — registered here exactly the
	// way a new handlers_<domain>.go file would do it.
	const fakeCat = model.Category("test_fake_category")
	var got struct {
		kind   probe.Kind
		code   int
		called bool
	}
	register(fakeCat, func(_ *Pipeline, kind probe.Kind, code int, _ map[byte]interface{}) {
		got.kind, got.code, got.called = kind, code, true
	})
	t.Cleanup(func() { delete(registry, fakeCat) })

	// Route through the registry the same way dispatch does after Classify.
	_, p := newGlue(t)
	registry[fakeCat](p, probe.KindEvent, 4242, map[byte]interface{}{})
	if !got.called || got.kind != probe.KindEvent || got.code != 4242 {
		t.Fatalf("fake handler not routed: %+v", got)
	}

	// Every category codes.json PRODUCES must have a registered handler — else it silently
	// classifies into the void (009). Derived from the embedded map (not a hand-list) so a
	// new category can't drift out of coverage: adding a code to codes.json without a
	// handler now fails this test (019, replacing a stale hand-maintained list).
	var cm struct {
		Entries []model.CodeMapEntry `json:"entries"`
	}
	if err := json.Unmarshal(data.CodesJSON, &cm); err != nil {
		t.Fatalf("codes.json parse: %v", err)
	}
	// Categories mapped in codes.json but intentionally classified-but-unhandled by the app
	// pipeline, WITH a reason. These are probe-coverage targets (feature 001 measured them)
	// that the live app does not consume; 019's codes.json-derived check surfaced them (the
	// old hand-list hid them by omission). Register a handler + drop the entry here if the app
	// ever needs one live.
	exempt := map[model.Category]string{
		model.CatEquipment:     "worn gear comes from the own-state snapshot (key 51), not event 30",
		model.CatMarketHistory: "app prices from live offers (R:81/82), not the R:95 history series",
		model.CatGoldPrice:     "gold/silver exchange price (R:248) is a probe metric, not shown live",
	}
	seen := map[model.Category]bool{}
	for _, e := range cm.Entries {
		if seen[e.Category] || exempt[e.Category] != "" {
			continue
		}
		seen[e.Category] = true
		if _, ok := registry[e.Category]; !ok {
			t.Fatalf("category %q from codes.json has no registered handler", e.Category)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no categories derived from codes.json — embed/parse broken")
	}
}

// 019 SC-002: the coverage loop FLAGS a category that codes.json produces but no handler
// serves — run on a synthetic entries slice so drift detection can't silently regress.
func TestRegistryCoverageDetectsUnhandled(t *testing.T) {
	entries := []model.CodeMapEntry{
		{Kind: "event", Code: 81, Category: model.CatWallet},                 // registered
		{Kind: "event", Code: 9999, Category: model.Category("phantom_019")}, // NOT registered
	}
	var flagged []model.Category
	for _, e := range entries {
		if _, ok := registry[e.Category]; !ok {
			flagged = append(flagged, e.Category)
		}
	}
	if len(flagged) != 1 || flagged[0] != model.Category("phantom_019") {
		t.Fatalf("coverage must flag exactly the unhandled phantom category, got %v", flagged)
	}
}

// 009 review (XI): the GUID bridge is hard-bounded at one wire guid per virtual
// container — a changing k54/k51 (character switch, hostile op-2 stream) prunes the
// stale reverse mapping instead of growing the map, and the old guid stops bridging.
func TestSelfContainerBridgeBounded(t *testing.T) {
	_, p := newGlue(t)
	for i := 0; i < 100; i++ {
		p.setSelfContainerGUID(strings.Repeat("a", 30)+string(rune('0'+i%10))+string(rune('0'+i/10)), SelfBagGUID)
	}
	if len(p.selfContainerGUIDs) > 2 { // bag entry (last one) + preset equipped entry
		t.Fatalf("bridge grew to %d entries (must stay ≤2)", len(p.selfContainerGUIDs))
	}
	// The stale previous bag guid no longer bridges.
	if p.isSelfBag(tBagGUID) {
		t.Fatal("previous character's bag guid must stop bridging after a rebind")
	}
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	const fakeCat = model.Category("test_dup_category")
	register(fakeCat, func(*Pipeline, probe.Kind, int, map[byte]interface{}) {})
	t.Cleanup(func() { delete(registry, fakeCat) })
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate registration must panic at startup, not shadow silently")
		}
	}()
	register(fakeCat, func(*Pipeline, probe.Kind, int, map[byte]interface{}) {})
}
