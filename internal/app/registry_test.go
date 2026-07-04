package app

// SC-002 proof (009): a new category handler is added with ONLY a register call —
// zero lines change in dispatch.go — and the dispatcher routes to it. Plus the
// duplicate-registration guard (a silent shadow would be a startup-order bug).

import (
	"testing"

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

	// All 13 real categories from codes.json are wired (a missing registration means
	// a category silently classified into the void).
	for _, cat := range []model.Category{
		model.CatItemValueEMV, model.CatInventory, model.CatBank, model.CatCharacterSpec,
		model.CatCurrentLocation, model.CatInventoryPut, model.CatInventoryDelete,
		model.CatSilver, model.CatLoot, model.CatGatherFishing, model.CatLootSource,
		model.CatLootMove, model.CatFame,
	} {
		if _, ok := registry[cat]; !ok {
			t.Fatalf("category %q has no registered handler", cat)
		}
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
