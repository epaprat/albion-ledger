package wailsadapter

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/photon"
)

// TestContainerSlotsAreItemIndices is the regression for the feature-003 inventory
// drop bug (research R2 / FR-001): AttachItemContainer(99) key 3 holds item catalog
// INDICES, not in-world object ids. The slots must flow straight to holdings via
// IngestContainerSlots — no object-id lookup that silently drops everything.
func TestContainerSlotsAreItemIndices(t *testing.T) {
	s, _, book := newHoldSvc(t)
	book.SetEMV(920, 0, 3360, 1000)

	guid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	payload := photon.BuildEventPacket(0, []photon.Field{
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: guid},
		{Key: 3, Type: photon.TypeArray | photon.TypeInteger, Val: []int32{920, 0, -1, 920}},
		{Key: 4, Type: photon.TypeInteger, Val: int32(4)},
		{Key: 252, Type: photon.TypeShort, Val: int16(99)},
	})
	var params map[byte]interface{}
	p := photon.NewPhotonParser(nil, nil, func(_ byte, pr map[byte]interface{}) { params = pr })
	if !p.ReceivePacket(payload) {
		t.Fatal("ReceivePacket false")
	}

	cGUID, owner, idxs, ok := capture.ContainerItems(params)
	if !ok {
		t.Fatal("ContainerItems not ok")
	}
	s.IngestContainerSlots(cGUID, owner, idxs)

	h := s.ListHoldings()
	if len(h) == 0 {
		t.Fatal("holdings empty — slots were dropped (the 003 object-id bug)")
	}
	if h[0].Item.Index != 920 || h[0].Valuation.Amount != 3360 {
		t.Fatalf("slot did not resolve as item index 920: %+v", h[0])
	}
}
