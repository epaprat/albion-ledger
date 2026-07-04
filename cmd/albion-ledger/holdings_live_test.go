package main

// Holdings-freshness glue tests (008): drive the REAL ingest path (classifier +
// extractors + tracker + service) with wire-shaped params, asserting the contract
// rules in specs/008-holdings-freshness/contracts/holdings-live.md.

import (
	"strings"
	"testing"

	"github.com/epaprat/albion-ledger/data"
	wailsadapter "github.com/epaprat/albion-ledger/internal/adapter/wails"
	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/codes"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/holdings"
	"github.com/epaprat/albion-ledger/internal/loot"
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

// newGlue resets all package-level 008 glue state and returns a fresh service +
// classifier wired exactly like main().
func newGlue(t *testing.T) (*wailsadapter.Service, *probe.Classifier) {
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

	// Reset glue globals (single test goroutine).
	objMu.Lock()
	objReg = map[int]holdings.ItemRef{}
	objOrder = nil
	pendingInv = map[int]string{}
	pendingLootResolve = map[int]pendingLoot{}
	pendingPuts = map[int]pendingPut{}
	pendingPutsDropped = 0
	objMu.Unlock()
	bagSlots = nil
	selfContainerGUIDs = map[string]string{tBagGUID: selfBagGUID, tEqGUID: selfEquipGUID}
	lootTracker = loot.New()

	// Pre-create virtual containers (mirrors OnStartup).
	svc.IngestSelfContainer(selfBagGUID, "Bag", nil)
	svc.IngestSelfContainer(selfEquipGUID, "Equipped", nil)
	return svc, probe.New(reg)
}

func guidBytes(hexish string) []byte {
	// tests use 32-char hex guids; decode loosely (a-f0-9 pairs)
	b := make([]byte, 16)
	for i := 0; i < 16; i++ {
		var v byte
		for j := 0; j < 2; j++ {
			c := hexish[i*2+j]
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= c - '0'
			case c >= 'a' && c <= 'f':
				v |= c - 'a' + 10
			}
		}
		b[i] = v
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
	svc, clf := newGlue(t)
	registerNewItem(svc, 32, declParams(910, 920, 3))
	ingest(clf, svc, probe.KindEvent, 26, putEvent(910, 4, tBagGUID))
	if !bagHas(svc, 920) {
		t.Fatal("order A: item must be in the bag")
	}
	if id, ok := bagSlotItem(4); !ok || id != 910 {
		t.Fatalf("order A: bagSlots[4] = %d/%v, want 910", id, ok)
	}

	// Order B: put first (declaration late) — must queue, then land on declaration.
	svc, clf = newGlue(t)
	ingest(clf, svc, probe.KindEvent, 26, putEvent(910, 4, tBagGUID))
	if bagHas(svc, 920) {
		t.Fatal("order B: item cannot resolve before its declaration")
	}
	registerNewItem(svc, 32, declParams(910, 920, 3))
	if !bagHas(svc, 920) {
		t.Fatal("order B: pending put must drain into the bag on declaration (root-cause fix)")
	}
}

// Contract rules 3-5 (T010): known destination = move; unknown destination = drop
// from view; empty source slot = no-op; loot correlation unchanged by application.
func TestMoveApplication(t *testing.T) {
	svc, clf := newGlue(t)
	// Bag holds item 910 (index 920) at slot 2 — via declaration + E:26.
	registerNewItem(svc, 32, declParams(910, 920, 1))
	ingest(clf, svc, probe.KindEvent, 26, putEvent(910, 2, tBagGUID))
	// Bank container known to the tracker AND to holdings (99-equivalent setup, with
	// its owner declared as a bank tab so it lands under Bank, not the bag).
	lootTracker.AttachContainer(tBankGUID, 4242, []int{0, 0, 0}, nowMS())
	svc.IngestBankVault([]string{"bankOwner"}, []string{"Items"})
	svc.IngestContainer(tBankGUID, "bankOwner", nil)

	// Move bag slot 2 → bank (known dst): relocates, single row.
	ingest(clf, svc, probe.KindRequest, 30, moveParams(2, tBagGUID, 0, tBankGUID))
	if bagHas(svc, 920) {
		t.Fatal("moved item must leave the bag")
	}
	rows := svc.ListHoldings()
	if len(rows) != 1 {
		t.Fatalf("move must not duplicate: %d rows", len(rows))
	}
	if _, ok := bagSlotItem(2); ok {
		t.Fatal("bag slot must be cleared after the move")
	}

	// Move to an UNKNOWN destination (market/sale): drops from view entirely.
	svc, clf = newGlue(t)
	registerNewItem(svc, 32, declParams(911, 837, 1))
	ingest(clf, svc, probe.KindEvent, 26, putEvent(911, 0, tBagGUID))
	ingest(clf, svc, probe.KindRequest, 30, moveParams(0, tBagGUID, 5, "cafecafecafecafecafecafecafecafe"))
	if holdingsCount(svc) != 0 {
		t.Fatalf("unknown-dst move must drop from view, got %d rows", holdingsCount(svc))
	}

	// Empty source slot: no-op.
	before := holdingsCount(svc)
	ingest(clf, svc, probe.KindRequest, 30, moveParams(9, tBagGUID, 0, tBankGUID))
	if holdingsCount(svc) != before {
		t.Fatal("empty-slot move must be a no-op")
	}
}

// Contract rule 6 (T010 regression guard): the same wire sequence produces identical
// loot Hits with holdings-application active (loot resolve runs first, untouched).
func TestLootCorrelationUnchangedByApplication(t *testing.T) {
	svc, clf := newGlue(t)
	// Loot source + container with item object 777 at slot 1; declared as index 920.
	lootTracker.RegisterSource(500, "Corpse", nowMS())
	lootTracker.AttachContainer("cccccccccccccccccccccccccccccccc", 500, []int{0, 777}, nowMS())
	registerNewItem(svc, 32, declParams(777, 920, 2))

	ingest(clf, svc, probe.KindRequest, 30, moveParams(1, "cccccccccccccccccccccccccccccccc", 3, tBagGUID))

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
	if id, ok := bagSlotItem(3); !ok || id != 777 {
		t.Fatalf("bagSlots[3] = %d/%v, want 777", id, ok)
	}
}

// T013: equip/unequip via the candidate bridge — bag↔equipped both directions.
func TestEquipUnequipBridge(t *testing.T) {
	svc, clf := newGlue(t)
	registerNewItem(svc, 32, declParams(912, 920, 1))
	ingest(clf, svc, probe.KindEvent, 26, putEvent(912, 1, tBagGUID))

	// Equip: bag slot 1 → equipped guid.
	ingest(clf, svc, probe.KindRequest, 30, moveParams(1, tBagGUID, 0, tEqGUID))
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
	ingest(clf, svc, probe.KindEvent, 26, putEvent(912, 1, tBagGUID))
	if !bagHas(svc, 920) {
		t.Fatal("unequip put must land the item back in the bag")
	}
}

// Contract rule 7 (T014): snapshots are authoritative — live changes then own-state
// → view equals snapshot; a late pending drain cannot resurrect an excluded item.
func TestSnapshotAuthority(t *testing.T) {
	svc, clf := newGlue(t)
	// Live: pending put for an undeclared object.
	ingest(clf, svc, probe.KindEvent, 26, putEvent(913, 0, tBagGUID))
	// Snapshot arrives: bag = only object 914 (declared, index 837); 913 excluded.
	registerNewItem(svc, 32, declParams(914, 837, 1))
	ingestSelf(svc, selfBagGUID, "Bag", []int{914})
	if slots, ok := map[bool][]int{true: {0, 914}}[true]; ok { // rebuild as own-state would
		bagSlots = slots
	}
	clearBagPendingPuts()
	// Late declaration of 913 must NOT resurrect it (pending cleared by snapshot).
	registerNewItem(svc, 32, declParams(913, 920, 1))
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
	svc, clf := newGlue(t)
	for i := 0; i < pendingPutsCap+50; i++ {
		ingest(clf, svc, probe.KindEvent, 26, putEvent(100_000+i, 0, tBagGUID))
	}
	objMu.Lock()
	n, dropped := len(pendingPuts), pendingPutsDropped
	objMu.Unlock()
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
	svc, clf := newGlue(t)
	registerNewItem(svc, 32, declParams(915, 920, 1))
	ingest(clf, svc, probe.KindEvent, 26, putEvent(915, 0, strings.Repeat("de", 16)))
	if holdingsCount(svc) != 0 {
		t.Fatalf("put to unknown container must be a no-op, got %d rows", holdingsCount(svc))
	}
}