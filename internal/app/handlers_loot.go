package app

// Loot correlation handlers (007): source announcements + the player's own move
// requests. Ordering contract rule 3: inside handleLootMove, loot correlation
// resolves BEFORE the holdings move-application — never as two registrations.

import (
	"log"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func init() {
	register(model.CatLootSource, handleLootSource)
	register(model.CatLootMove, handleLootMove)
}

// handleLootSource — NewLoot(98) / NewLootChest(393) / LootChestOpened(395).
func handleLootSource(p *Pipeline, _ probe.Kind, code int, params map[byte]interface{}) {
	if objID, name, ok := capture.LootSource(params, code); ok {
		p.lootTracker.RegisterSource(objID, name, p.nowMS())
		if p.debug {
			log.Printf("[flow] loot source: code=%d obj=%d name=%q", code, objID, name)
		}
	}
}

// handleLootMove — own item-move requests (op-30 single, op-39 take-all): loot
// correlation first, then the holdings move-application (008).
func handleLootMove(p *Pipeline, _ probe.Kind, code int, params map[byte]interface{}) {
	switch code {
	case 30:
		if guid, slot, ok := capture.MoveItem(params); ok {
			// Moves OUT OF the player's own bag can never be loot pickups — feeding
			// them to the tracker only pollutes its pending queue (live-seen
			// 2026-07-04). Gated on the CONFIRMED bag guid only: the key-51 equipped
			// CANDIDATE must not silently suppress loot resolution if it turns out
			// to be some other container.
			if v, own := p.virtualContainer(guid); !own || v != SelfBagGUID {
				hits := p.lootTracker.ResolveMove(guid, slot, p.nowMS())
				if p.debug {
					pend, exp, capd := p.lootTracker.Stats()
					log.Printf("[flow] move: guid=%s slot=%d hits=%d (pending=%d expired=%d capdrop=%d)", guid, slot, len(hits), pend, exp, capd)
				}
				p.emitLootHits(hits)
			}
			// Holdings move-application (008) — AFTER loot correlation, never before.
			dstSlot, dstGUID, hasDst := capture.MoveDest(params)
			p.applyMoveToHoldings(guid, slot, dstGUID, dstSlot, hasDst)
		}
	case 39:
		if guid, ids, ok := capture.MoveGivenItems(params); ok {
			if v, own := p.virtualContainer(guid); !own || v != SelfBagGUID { // bag-only skip, see op-30
				hits := p.lootTracker.ResolveMoveGiven(guid, ids, p.nowMS())
				if p.debug {
					log.Printf("[flow] move-given: guid=%s ids=%d hits=%d", guid, len(ids), len(hits))
				}
				p.emitLootHits(hits)
			}
			dstGUID, hasDst := capture.MoveGivenDest(params)
			for _, id := range ids {
				p.applyMovedObject(id, guid, dstGUID, -1, hasDst)
			}
		}
	}
}
