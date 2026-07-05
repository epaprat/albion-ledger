package app

// Holdings-freshness glue tests (008): drive the REAL dispatch path (classifier +
// extractors + tracker + service) with wire-shaped params, asserting the contract
// rules in specs/008-holdings-freshness/contracts/holdings-live.md.
//
// MECHANICALLY MOVED from cmd/albion-ledger (009 stage B): fixtures, scenarios and
// assertions are verbatim; only the plumbing changed (package globals → Pipeline
// fields, ingest()/registerNewItem() free functions → methods). This is the single
// recorded FR-005 deviation (specs/009-handler-registry/research.md Decision 5).

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/epaprat/albion-ledger/data"
	wailsadapter "github.com/epaprat/albion-ledger/internal/adapter/wails"
	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/codes"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

const testCat = `{"items":[
  {"index":920,"uniqueName":"T7_WOOD","name":"Ashenbark Logs"},
  {"index":837,"uniqueName":"T8_MEAL","name":"Avalonian Stew"}
]}`

const (
	tBagGUID  = "0a942a2c00000000000000000000000b"
	tEqGUID   = "299edbed00000000000000000000000e"
	tBankGUID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func nowMS() int64 { return time.Now().UnixMilli() }

// newGlue returns a fresh service + Pipeline wired exactly like main(), with the
// self-container bridge preset (the 008 tests exercise handlers, not the Join).
func newGlue(t *testing.T) (*wailsadapter.Service, *Pipeline) {
	t.Helper()
	c, err := catalog.New([]byte(testCat))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := codes.New(data.CodesJSON)
	if err != nil {
		t.Fatal(err)
	}
	book := valuation.NewBook()
	val := valuation.NewValuer(book, model.DefaultStaleAfterMS)
	svc := wailsadapter.NewService(c, book, val, nil, 100, nowMS)

	p := New(svc, probe.New(reg), nil, testSpecNames{}, nowMS, false)
	p.selfContainerGUIDs = map[string]string{tBagGUID: SelfBagGUID, tEqGUID: SelfEquipGUID}

	// Pre-create virtual containers (mirrors OnStartup): pinned, not-yet-observed.
	svc.EnsureSelfContainer(SelfBagGUID, "Bag")
	svc.EnsureSelfContainer(SelfEquipGUID, "Equipped")
	return svc, p
}

func guidBytes(hexish string) []byte {
	b, err := hex.DecodeString(hexish)
	if err != nil {
		panic("malformed test guid: " + hexish) // surface typo'd constants loudly
	}
	return b
}

func putEvent(objID, slot int, guid string) map[byte]interface{} {
	return map[byte]interface{}{0: int32(objID), 1: int32(slot), 2: guidBytes(guid), 252: int16(26)}
}

func declParams(objID, itemIdx, qty int) map[byte]interface{} {
	return map[byte]interface{}{0: int32(objID), 1: int32(itemIdx), 2: int32(qty), 252: int16(32)}
}

func moveParams(srcSlot int, srcGUID string, dstSlot int, dstGUID string) map[byte]interface{} {
	return map[byte]interface{}{
		0: int32(srcSlot), 1: guidBytes(srcGUID),
		3: int32(dstSlot), 4: guidBytes(dstGUID),
		253: int16(30),
	}
}

func bagHas(svc *wailsadapter.Service, itemIdx int) bool {
	for _, r := range svc.ListHoldings() {
		if r.Item.Index == itemIdx && r.Group == "Bag" {
			return true
		}
	}
	return false
}

func holdingsCount(svc *wailsadapter.Service) int { return len(svc.ListHoldings()) }

// Contract rules 1-2 (T007/T009): an E:26 put with a LATE declaration must not be
// lost — both orderings converge to the same bag; bridged guid lands in self-bag.
func TestPutWithLateDeclarationBothOrders(t *testing.T) {
	// Order A: declaration first, then put.
	svc, p := newGlue(t)
	p.registerNewItem(32, declParams(910, 920, 3))
	p.dispatch(probe.KindEvent, 26, putEvent(910, 4, tBagGUID))
	if !bagHas(svc, 920) {
		t.Fatal("order A: item must be in the bag")
	}
	if id, ok := p.bagSlotItem(4); !ok || id != 910 {
		t.Fatalf("order A: bagSlots[4] = %d/%v, want 910", id, ok)
	}

	// Order B: put first (declaration late) — must queue, then land on declaration.
	svc, p = newGlue(t)
	p.dispatch(probe.KindEvent, 26, putEvent(910, 4, tBagGUID))
	if bagHas(svc, 920) {
		t.Fatal("order B: item cannot resolve before its declaration")
	}
	p.registerNewItem(32, declParams(910, 920, 3))
	if !bagHas(svc, 920) {
		t.Fatal("order B: pending put must drain into the bag on declaration (root-cause fix)")
	}
}

// Contract rules 3-5 (T010): known destination = move; unknown destination = drop
// from view; empty source slot = no-op; loot correlation unchanged by application.
func TestMoveApplication(t *testing.T) {
	svc, p := newGlue(t)
	// Bag holds item 910 (index 920) at slot 2 — via declaration + E:26.
	p.registerNewItem(32, declParams(910, 920, 1))
	p.dispatch(probe.KindEvent, 26, putEvent(910, 2, tBagGUID))
	// Bank container known to the tracker AND to holdings (99-equivalent setup, with
	// its owner declared as a bank tab so it lands under Bank, not the bag).
	p.lootTracker.AttachContainer(tBankGUID, 4242, []int{0, 0, 0}, nowMS())
	svc.IngestBankVault([]string{"bankOwner"}, []string{"Items"})
	svc.IngestContainer(tBankGUID, "bankOwner", nil)

	// Move bag slot 2 → bank (known dst): relocates, single row.
	p.dispatch(probe.KindRequest, 30, moveParams(2, tBagGUID, 0, tBankGUID))
	if bagHas(svc, 920) {
		t.Fatal("moved item must leave the bag")
	}
	rows := svc.ListHoldings()
	if len(rows) != 1 {
		t.Fatalf("move must not duplicate: %d rows", len(rows))
	}
	if _, ok := p.bagSlotItem(2); ok {
		t.Fatal("bag slot must be cleared after the move")
	}

	// Move to an UNKNOWN destination (market/sale): drops from view entirely.
	svc, p = newGlue(t)
	p.registerNewItem(32, declParams(911, 837, 1))
	p.dispatch(probe.KindEvent, 26, putEvent(911, 0, tBagGUID))
	p.dispatch(probe.KindRequest, 30, moveParams(0, tBagGUID, 5, "cafecafecafecafecafecafecafecafe"))
	if holdingsCount(svc) != 0 {
		t.Fatalf("unknown-dst move must drop from view, got %d rows", holdingsCount(svc))
	}

	// Empty source slot: no-op.
	before := holdingsCount(svc)
	p.dispatch(probe.KindRequest, 30, moveParams(9, tBagGUID, 0, tBankGUID))
	if holdingsCount(svc) != before {
		t.Fatal("empty-slot move must be a no-op")
	}
}

// Contract rule 6 (T010 regression guard): the same wire sequence produces identical
// loot Hits with holdings-application active (loot resolve runs first, untouched).
func TestLootCorrelationUnchangedByApplication(t *testing.T) {
	svc, p := newGlue(t)
	// Loot source + container with item object 777 at slot 1; declared as index 920.
	p.lootTracker.RegisterSource(500, "Corpse", nowMS())
	p.lootTracker.AttachContainer("cccccccccccccccccccccccccccccccc", 500, []int{0, 777}, nowMS())
	p.registerNewItem(32, declParams(777, 920, 2))

	p.dispatch(probe.KindRequest, 30, moveParams(1, "cccccccccccccccccccccccccccccccc", 3, tBagGUID))

	// Loot flow event recorded exactly once, with identity from the registry.
	var lootRows int
	for _, e := range svc.ListFlow() {
		if e.Kind == model.FlowLoot {
			lootRows++
			if e.ItemDisplayName != "Ashenbark Logs" || e.Count != 2 {
				t.Fatalf("loot row wrong: %+v", e)
			}
		}
	}
	if lootRows != 1 {
		t.Fatalf("loot rows = %d, want exactly 1 (correlation unchanged)", lootRows)
	}
	// And the holdings application placed the item into the bag at slot 3.
	if !bagHas(svc, 920) {
		t.Fatal("looted item must land in the bag view")
	}
	if id, ok := p.bagSlotItem(3); !ok || id != 777 {
		t.Fatalf("bagSlots[3] = %d/%v, want 777", id, ok)
	}
}

// T013: equip/unequip via the candidate bridge — bag↔equipped both directions.
func TestEquipUnequipBridge(t *testing.T) {
	svc, p := newGlue(t)
	p.registerNewItem(32, declParams(912, 920, 1))
	p.dispatch(probe.KindEvent, 26, putEvent(912, 1, tBagGUID))

	// Equip: bag slot 1 → equipped guid.
	p.dispatch(probe.KindRequest, 30, moveParams(1, tBagGUID, 0, tEqGUID))
	var equipped bool
	for _, r := range svc.ListHoldings() {
		if r.Item.Index == 920 && r.Group == "Equipped" {
			equipped = true
		}
	}
	if !equipped || bagHas(svc, 920) {
		t.Fatal("equip must move the item bag → equipped")
	}

	// Unequip: equipped → bag. Source slot resolution for the equipped container is
	// not slot-mapped (candidate guid, no slot map) — the wire also sends E:26 into
	// the bag on unequip; simulate that path.
	p.dispatch(probe.KindEvent, 26, putEvent(912, 1, tBagGUID))
	if !bagHas(svc, 920) {
		t.Fatal("unequip put must land the item back in the bag")
	}
}

// Contract rule 7 (T014): snapshots are authoritative — live changes then own-state
// → view equals snapshot; a late pending drain cannot resurrect an excluded item.
func TestSnapshotAuthority(t *testing.T) {
	svc, p := newGlue(t)
	// Live: pending put for an undeclared object.
	p.dispatch(probe.KindEvent, 26, putEvent(913, 0, tBagGUID))
	// Snapshot arrives: bag = only object 914 (declared, index 837); 913 excluded.
	p.registerNewItem(32, declParams(914, 837, 1))
	p.ingestSelf(SelfBagGUID, "Bag", []int{914})
	p.bagSlots = []int{0, 914} // rebuilt from key 55, as the own-state handler does
	p.clearSelfPendingPuts()
	// Late declaration of 913 must NOT resurrect it (pending cleared by snapshot).
	p.registerNewItem(32, declParams(913, 920, 1))
	if bagHas(svc, 920) {
		t.Fatal("snapshot-excluded item resurrected by late drain (contract rule 7)")
	}
	if !bagHas(svc, 837) {
		t.Fatal("snapshot content must be present")
	}
	// Sanity: exactly the snapshot's one bag item.
	var bagRows int
	for _, r := range svc.ListHoldings() {
		if r.Group == "Bag" {
			bagRows++
		}
	}
	if bagRows != 1 {
		t.Fatalf("bag rows = %d, want exactly the snapshot's 1", bagRows)
	}
}

// T016 (Principle XI): pendingPuts stays within cap under a flood of undeclared puts,
// overflow/expiry are counted, and a fresh put still lands after TTL sweep frees room.
func TestPendingPutsBounded(t *testing.T) {
	_, p := newGlue(t)
	const pendingPutsCap = 256 // bound lives in pending.Map; asserted via Len/Dropped
	for i := 0; i < pendingPutsCap+50; i++ {
		p.dispatch(probe.KindEvent, 26, putEvent(100_000+i, 0, tBagGUID))
	}
	p.objMu.Lock()
	n, dropped := p.pendingPuts.Len(), p.pendingPuts.Dropped()
	p.objMu.Unlock()
	if n > pendingPutsCap {
		t.Fatalf("pendingPuts %d exceeds cap %d", n, pendingPutsCap)
	}
	if dropped != 50 {
		t.Fatalf("dropped = %d, want 50 (counted, not silent)", dropped)
	}
}

// Guard against future accidental reintroduction of the "unknown container → Bag"
// default (008 T004): a put to a random guid must create nothing.
func TestUnknownContainerPutCreatesNothing(t *testing.T) {
	svc, p := newGlue(t)
	p.registerNewItem(32, declParams(915, 920, 1))
	p.dispatch(probe.KindEvent, 26, putEvent(915, 0, strings.Repeat("de", 16)))
	if holdingsCount(svc) != 0 {
		t.Fatalf("put to unknown container must be a no-op, got %d rows", holdingsCount(svc))
	}
}

// Review fix: an E:26 into an UNTRACKED container (quick-deposit into an unopened
// bank tab) must still remove the item from its previous container — the old silent
// no-op left it stale in the bag, the exact bug class 008 fights.
func TestPutToUntrackedContainerDropsFromOldView(t *testing.T) {
	svc, p := newGlue(t)
	p.registerNewItem(32, declParams(916, 920, 1))
	p.dispatch(probe.KindEvent, 26, putEvent(916, 2, tBagGUID))
	if !bagHas(svc, 920) {
		t.Fatal("setup: item must start in the bag")
	}
	// Server-driven transfer to a never-attached container.
	p.dispatch(probe.KindEvent, 26, putEvent(916, 0, strings.Repeat("ab", 16)))
	if bagHas(svc, 920) || holdingsCount(svc) != 0 {
		t.Fatal("item must leave the bag when put into an untracked container")
	}
	if _, ok := p.bagSlotItem(2); ok {
		t.Fatal("bag slot map must drop the departed item")
	}
}

// Review fix (IV/XI): hostile slot indexes must not balloon the bag slot map — one
// corrupt E:26/op-30 with slot=2^31 would otherwise allocate gigabytes.
func TestBagSlotsBoundedAgainstHostileSlots(t *testing.T) {
	_, p := newGlue(t)
	p.registerNewItem(32, declParams(917, 920, 1))
	p.dispatch(probe.KindEvent, 26, putEvent(917, 1<<30, tBagGUID))
	if len(p.bagSlots) > maxBagSlots {
		t.Fatalf("bagSlots grew to %d (hostile slot accepted)", len(p.bagSlots))
	}
	// The sane path keeps working: a legal slot still lands.
	p.dispatch(probe.KindEvent, 26, putEvent(917, 3, tBagGUID))
	if id, ok := p.bagSlotItem(3); !ok || id != 917 {
		t.Fatalf("legal slot rejected after hostile attempt: %d/%v", id, ok)
	}
}

// Review fix: snapshot authority must be symmetric — an equipped-targeted pending
// put must not resurrect an item the equipped snapshot excluded.
func TestSnapshotAuthorityEquipped(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindEvent, 26, putEvent(918, 0, tEqGUID)) // undeclared → pending
	p.registerNewItem(32, declParams(919, 837, 1))
	p.ingestSelf(SelfEquipGUID, "Equipped", []int{919}) // authoritative, excludes 918
	p.clearSelfPendingPuts()
	p.registerNewItem(32, declParams(918, 920, 1)) // late declaration
	for _, r := range svc.ListHoldings() {
		if r.Item.Index == 920 && r.Group == "Equipped" {
			t.Fatal("snapshot-excluded item resurrected into Equipped")
		}
	}
}

// Review fix: a deposit into a bank tab holdings knows (snapshotted) must relocate
// the item even when the loot tracker never attached (or TTL-swept) that guid —
// destination knowledge lives in holdings, not the 10-minute loot cache.
func TestMoveToHoldingsKnownButTrackerUnknownDst(t *testing.T) {
	svc, p := newGlue(t)
	p.registerNewItem(32, declParams(921, 920, 1))
	p.dispatch(probe.KindEvent, 26, putEvent(921, 0, tBagGUID))
	svc.IngestBankVault([]string{"bankOwner"}, []string{"Items"})
	svc.IngestContainer(tBankGUID, "bankOwner", nil) // holdings knows; tracker does NOT
	p.dispatch(probe.KindRequest, 30, moveParams(0, tBagGUID, 1, tBankGUID))
	if bagHas(svc, 920) {
		t.Fatal("item must leave the bag")
	}
	var inBank bool
	for _, r := range svc.ListHoldings() {
		if r.Item.Index == 920 && r.Group == "Items" {
			inBank = true
		}
	}
	if !inBank {
		t.Fatal("item must land in the snapshotted bank tab (tracker TTL must not matter)")
	}
}
// testSpecNames is a trivial resolver for the pipeline tests (011).
type testSpecNames struct{}

func (testSpecNames) Resolve(id int) (string, string, string, bool) {
	switch id {
	case 22:
		return "Combat Axes", "Combat", "Axes", true
	case 999:
		return "Test Node", "Combat", "Test", true
	}
	return "", "", "", false
}

func (testSpecNames) All() []model.SpecNodeCatalog {
	return []model.SpecNodeCatalog{
		{ID: 22, Name: "Combat Axes", Category: "Combat", Subcategory: "Axes", FameToMax: 1000},
		{ID: 999, Name: "Test Node", Category: "Combat", Subcategory: "Test", FameToMax: 1000},
	}
}
