package app

// Holdings handlers: container snapshots (99), bank vault info, and the live
// put/delete events (26/27) with the 008 pending/bridge glue.

import (
	"log"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func init() {
	register(model.CatInventory, handleAttachContainer)
	register(model.CatBank, handleBankVault)
	register(model.CatInventoryPut, handleInventoryPut)
	register(model.CatInventoryDelete, handleInventoryDelete)
}

// handleAttachContainer — AttachItemContainer (99): key-3 slots are in-world object
// ids, resolved to item type+quality via the object registry (New*Item declarations).
func handleAttachContainer(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	// The SLOT-INDEXED view (empties preserved) carries the container's source object,
	// which classifies it: a container linked to an announced LOOT source (a corpse/chest)
	// is foreign and must feed ONLY the loot tracker, never holdings — else the items the
	// player merely VIEWED in a mob's bag land under their own BAG though never taken
	// (live-seen 2026-07-10). Banks (src a small constant, never announced as loot) and the
	// real bag (op-2, not here) still reach holdings.
	guid, srcObjID, slots, isContainer := capture.ContainerSlots(params)
	isLoot := isContainer && p.lootTracker.IsLootSource(srcObjID, p.nowMS())
	if cGUID, ownerGUID, objIDs, ok := capture.ContainerItems(params); ok && !isLoot {
		p.sink.IngestContainer(cGUID, ownerGUID, p.resolveObjects(objIDs))
	}
	// Loot correlation: source link + slot map, and attaching may resolve moves that
	// arrived early. Live-verified 2026-07-03: key0 IS the source obj id for open-world
	// loot containers; bank containers carry a small constant (6) — harmless, the tracker
	// excludes bank-sized containers from loot resolution.
	if isContainer {
		if p.debug {
			log.Printf("[flow] attach: guid=%s src=%d slots=%d", guid, srcObjID, len(slots))
		}
		p.emitLootHits(p.lootTracker.AttachContainer(guid, srcObjID, slots, p.nowMS()))
	}
}

// handleBankVault — BankVaultInfo: declares the bank tabs (owner GUIDs + names).
func handleBankVault(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if owners, tabNames, ok := capture.BankVault(params); ok {
		p.sink.IngestBankVault(owners, tabNames)
	}
}

// handleInventoryPut — event 26: item added/moved into a container (live, 008).
func handleInventoryPut(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if objID, slot, cGUID, ok := capture.PutItem(params); ok {
		target := cGUID
		if v, bridged := p.virtualContainer(cGUID); bridged {
			target = v
			if target == SelfBagGUID && slot >= 0 {
				p.bagSlotSet(slot, objID) // keep the live bag slot map aligned (008)
			}
		}
		if ref, ok := p.resolveObj(objID); ok {
			if !p.sink.IngestPutItem(target, objID, ref) {
				// Destination untracked (e.g. quick-deposit into an unopened bank
				// tab): the item still LEFT wherever it was — drop it from view so
				// it can't linger stale; snapshots restore it where it truly lives.
				p.sink.IngestDeleteItem(objID)
				p.bagSlotClear(objID)
			}
		} else {
			// Declaration not seen yet — queue instead of dropping (008 root-cause
			// fix): drained in registerNewItem, expired entries counted.
			p.queuePendingPut(objID, target)
		}
	}
}

// handleInventoryDelete — event 27: item removed from a container (live).
func handleInventoryDelete(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if objID, ok := capture.DeleteItem(params); ok {
		p.sink.IngestDeleteItem(objID)
		p.bagSlotClear(objID)
	}
}
