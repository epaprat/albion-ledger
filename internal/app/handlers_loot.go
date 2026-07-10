package app

// Loot correlation handlers (007): source announcements + the player's own move
// requests. Ordering contract rule 3: inside handleLootMove, loot correlation
// resolves BEFORE the holdings move-application — never as two registrations.

import (
	"encoding/hex"
	"fmt"
	"log"
	"sort"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

// dumpMoveParams renders an op-30 move request's raw params (key→value) for the
// phantom-move investigation: []byte as short hex, everything else via %v. Temporary
// diagnostic — removed once the slot→object resolution is confirmed.
func dumpMoveParams(params map[byte]interface{}) string {
	keys := make([]int, 0, len(params))
	for k := range params {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := params[byte(k)]
		if b, ok := v.([]byte); ok {
			h := hex.EncodeToString(b)
			if len(h) > 16 {
				h = h[:16] + "…"
			}
			parts = append(parts, fmt.Sprintf("%d=hex:%s", k, h))
			continue
		}
		parts = append(parts, fmt.Sprintf("%d=%v", k, v))
	}
	return "{" + fmt.Sprintf("%v", parts) + "}"
}

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
		if p.debug {
			log.Printf("[hold] op30 raw params: %s", dumpMoveParams(params))
		}
		if guid, slot, ok := capture.MoveItem(params); ok {
			// Moves OUT OF the player's own bag can never be loot pickups — feeding
			// them to the tracker only pollutes its pending queue (live-seen
			// 2026-07-04). isSelfBag gates on the CONFIRMED bag guid only: the key-51
			// equipped CANDIDATE must not silently suppress loot resolution if it
			// turns out to be some other container.
			if !p.isSelfBag(guid) {
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
			if !p.isSelfBag(guid) { // bag-only skip, see op-30
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
