package app

// Flow (earnings) handlers: silver, gather/fishing, fame, and the 279 broadcast
// observability stub (005/007).

import (
	"fmt"
	"log"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func init() {
	register(model.CatSilver, handleSilver)
	register(model.CatGatherFishing, handleGatherFishing)
	register(model.CatFame, handleFame)
	register(model.CatLoot, handleLootBroadcast)
}

// handleSilver — TakeSilver (62): own net silver (005 US1).
func handleSilver(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if target, obj, net, ok := capture.SilverEvent(params); ok {
		if p.debug {
			log.Printf("[flow] silver evt: obj=%d target=%d net=%d self=%d match=%v", obj, target, net, p.selfObjID, p.isSelfObj(obj))
		}
		// key0 (obj) is the RECEIVING player (self); key2 is the mob/target entity.
		// Live-verified 2026-07-01: obj==self across a session, target varies per mob.
		if p.isSelfObj(obj) && net != 0 {
			// Per-event nonce: identical yields are common (same mob type, AoE kills in
			// one wire tick) and key 1 (timestamp) is not guaranteed — without the seq,
			// equal-net pickups collapse into one id and undercount. Wire-level
			// retransmits are already deduped by the Photon reliable layer.
			ts, _ := capture.IntParam(params, 1)
			id := fmt.Sprintf("sv:%d:%d:%d", ts, net, p.nextFlowSeq())
			p.sink.IngestSilver(id, net, p.nowMS(), "")
		}
	}
}

// handleGatherFishing — HarvestFinished (61) + RewardGranted (267): own gathers (005 US3).
func handleGatherFishing(p *Pipeline, _ probe.Kind, code int, params map[byte]interface{}) {
	switch code {
	case 61:
		gatherer, itemID, amount, ok := capture.HarvestEvent(params)
		if ok && !p.isSelfObj(gatherer) && p.debug {
			log.Printf("[flow] harvest DROPPED (self unknown/other): gatherer=%d self=%d item=%d amount=%d", gatherer, p.selfObjID, itemID, amount)
		}
		if ok && p.isSelfObj(gatherer) {
			// Each harvest tick on the SAME node is a distinct gain (a node yields
			// several charges), so the dedup id must be per-tick unique — keying by
			// node+item collapsed multiple charges into one (~3× undercount). Photon
			// transport already drops re-delivered packets, so a monotonic seq is safe.
			node, _ := capture.IntParam(params, 3)
			id := fmt.Sprintf("hv:%d:%d:%d", node, itemID, p.nextFlowSeq())
			if p.debug {
				std, _ := capture.IntParam(params, 5)
				bonus, _ := capture.IntParam(params, 6)
				prem, _ := capture.IntParam(params, 7)
				log.Printf("[flow] harvest: node=%d item=%d std=%d bonus=%d prem=%d total=%d", node, itemID, std, bonus, prem, amount)
			}
			p.sink.IngestGather(id, itemID, 0, amount, p.nowMS(), "")
		}
	case 267:
		if itemID, qty, ok := capture.RewardEvent(params); ok {
			id := fmt.Sprintf("rw:%d:%d:%d", itemID, qty, p.nextFlowSeq())
			if p.debug {
				log.Printf("[flow] reward: item=%d qty=%d", itemID, qty)
			}
			p.sink.IngestGather(id, itemID, 0, qty, p.nowMS(), "")
		}
		// code 38 is unverified (see codes.json / 005 tasks T031) — intentionally ignored.
	}
}

// handleFame — UpdateFame (82): fame is inherently own (005 US4).
func handleFame(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if fame, ok := capture.FameEvent(params); ok && fame > 0 {
		// Per-event nonce: key 1 (running total) is unverified — if absent, every
		// id would be "fm:0" and all fame after the first tick would be dropped.
		total, _ := capture.IntParam(params, 1)
		id := fmt.Sprintf("fm:%d:%d", total, p.nextFlowSeq())
		p.sink.IngestFame(id, fame, p.nowMS())
	}
}

// handleLootBroadcast — OtherGrabbedLoot (279): observability only. Own ITEM loot is
// counted by the 007 correlation (own move request × loot source); ingesting the 279
// broadcast too would double-count the same pickup under a different dedup id. Own
// SILVER grabs already arrive via TakeSilver(62). Kept classified so probe coverage
// still observes the category.
func handleLootBroadcast(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if p.debug {
		if looter, isSilver, itemID, amount, ok := capture.LootGrab(params); ok {
			log.Printf("[flow] loot evt (broadcast, not ingested): looter=%q item=%d amt=%d isSilver=%v", looter, itemID, amount, isSilver)
		}
	}
}
