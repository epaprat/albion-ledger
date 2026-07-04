package app

// Own-state + location handlers: the login/zone snapshot (op-2 with key 55) and the
// city notification (event 163). Per-Join self identity is NOT here — it is the fixed
// pre-dispatch step in OnResponse (ordering contract rule 1).

import (
	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func init() {
	register(model.CatCharacterSpec, handleOwnState)
	register(model.CatCurrentLocation, handleCurrentCity)
}

// handleOwnState — own-state (op-2 with key 55): the login bag (key 55) + equipped
// (key 52). NOTE: key 55 is the bag and key 52 the worn set — neither is masteries
// (the earlier reading was wrong); the real spec source is TBD, so Spec stays empty.
func handleOwnState(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if objIDs, ok := capture.OwnInventory(params); ok {
		p.ingestSelf(SelfBagGUID, "Bag", objIDs)
	}
	if objIDs, ok := capture.OwnEquipped(params); ok {
		p.ingestSelf(SelfEquipGUID, "Equipped", objIDs)
	}
	// Full snapshot is authoritative (008): rebuild the live bag slot map from key 55
	// and drop self-targeted pending puts — an item the snapshot excluded must not be
	// resurrected by a late declaration drain.
	if slots, ok := capture.OwnInventorySlots(params); ok {
		p.bagSlots = slots
		p.clearSelfPendingPuts()
	}
}

// handleCurrentCity — notification event 163: "you entered <city>".
func handleCurrentCity(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if city, ok := capture.CurrentCity(params); ok {
		p.sink.SetCurrentCity(city)
		p.sink.SetZone(city) // readable city name overrides the raw cluster id for flow zone
	}
}
