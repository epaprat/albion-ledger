package capture

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/loot"
	"github.com/epaprat/albion-ledger/internal/photon"
)

// TestLootPipelineEndToEnd drives REAL wire bytes through the extractor + tracker
// pipeline exactly as main wires them: loot-source announcement (98) → container
// attach (99, slot-indexed) → the player's own move request (op-30) → one Hit with
// the right item object and source label. Then the bank counter-case: a container
// with an unannounced owner never produces a Hit (US2).
func TestLootPipelineEndToEnd(t *testing.T) {
	tr := loot.New()
	guid := []byte{7, 7, 7, 7, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// 1. NewLoot(98): corpse 4242 announced.
	p := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(4242)},
		{Key: 3, Type: photon.TypeString, Val: "Keeper Earthmother"},
		{Key: 252, Type: photon.TypeShort, Val: int16(98)},
	})
	if objID, name, ok := LootSource(p, 98); ok {
		tr.RegisterSource(objID, name, 1000)
	} else {
		t.Fatal("LootSource failed")
	}

	// 2. AttachItemContainer(99): corpse container, item object 910 in slot 1.
	p = decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(4242)},
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: guid},
		{Key: 3, Type: photon.TypeArray | photon.TypeInteger, Val: []int32{0, 910}},
		{Key: 252, Type: photon.TypeShort, Val: int16(99)},
	})
	cg, src, slots, ok := ContainerSlots(p)
	if !ok {
		t.Fatal("ContainerSlots failed")
	}
	if hits := tr.AttachContainer(cg, src, slots, 1001); len(hits) != 0 {
		t.Fatalf("attach alone must not hit: %v", hits)
	}

	// 3. The player's own InventoryMoveItem request (op-30): slot 1 out of the corpse.
	payload := photon.BuildRequestPacket(30, []photon.Field{
		{Key: 0, Type: photon.TypeByte, Val: byte(1)},
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: guid},
		{Key: 253, Type: photon.TypeShort, Val: int16(30)},
	})
	var req map[byte]interface{}
	pr := photon.NewPhotonParser(func(_ byte, params map[byte]interface{}) { req = params }, nil, nil)
	if !pr.ReceivePacket(payload) {
		t.Fatal("request packet not parsed")
	}
	mg, slot, ok := MoveItem(req)
	if !ok {
		t.Fatal("MoveItem failed")
	}
	hits := tr.ResolveMove(mg, slot, 1002)
	if len(hits) != 1 || hits[0].ItemObjID != 910 || hits[0].Source != "Keeper Earthmother" {
		t.Fatalf("hits = %+v, want one {910, Keeper Earthmother}", hits)
	}

	// 4. Bank counter-case (US2): same shape, owner 9999 never announced → no Hit,
	// no pending (container is known).
	bankGUID := []byte{8, 8, 8, 8, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	p = decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(9999)},
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: bankGUID},
		{Key: 3, Type: photon.TypeArray | photon.TypeInteger, Val: []int32{111, 222}},
		{Key: 252, Type: photon.TypeShort, Val: int16(99)},
	})
	bg, bsrc, bslots, _ := ContainerSlots(p)
	tr.AttachContainer(bg, bsrc, bslots, 1003)
	if hits := tr.ResolveMove(bg, 0, 1004); len(hits) != 0 {
		t.Fatalf("bank move must not produce loot: %v", hits)
	}
	if pend, _, _ := tr.Stats(); pend != 0 {
		t.Fatalf("bank move must not queue, pending = %d", pend)
	}
}