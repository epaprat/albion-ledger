package app

// Destiny Board handlers (feature 011): the full snapshot (E:154, every Join), live
// progress deltas (E:153) and node completions (E:152) drive the character's skill
// tree in the Spec panel. All three are self-filtered — another player's achievement
// broadcast (if any) must never leak in.

import (
	"log"
	"strconv"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/specboard"
)

func init() {
	register(model.CatSpecSnapshot, handleSpecSnapshot)
	register(model.CatSpecDelta, handleSpecDelta)
	register(model.CatSpecDone, handleSpecDone)
}

// specSelfMatches gates all three messages on k0 == self (005 isSelfObj pattern).
func (p *Pipeline) specSelfMatches(params map[byte]interface{}) bool {
	self, ok := capture.SpecSelf(params)
	return ok && p.isSelfObj(self)
}

// handleSpecSnapshot — E:154: full REPLACE (the authority).
func handleSpecSnapshot(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if !p.specSelfMatches(params) {
		return
	}
	updates, ok := capture.AchievementSnapshot(params)
	if !ok {
		return
	}
	nodes := make([]specboard.Node, len(updates))
	for i, u := range updates {
		nodes[i] = specboard.Node{ID: u.ID, Level: u.Level, Progress: u.Progress, Fame: u.Fame}
	}
	// The board is sent as several E:154 packets per Join; the first after a Join
	// clears (authority), the rest of the burst merge into it (live-seen 75+75+36).
	if p.specReplacePending {
		p.board.ReplaceAll(nodes)
		p.specReplacePending = false
	} else {
		p.board.MergeAll(nodes)
	}
	p.emitSpec()
	if p.debug {
		p2, _ := p.board.Totals()
		log.Printf("[spec] snapshot n=%d total=%d", len(nodes), p2)
	}
}

// handleSpecDelta — E:153: single-node upsert (level optional).
func handleSpecDelta(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if !p.specSelfMatches(params) {
		return
	}
	u, ok := capture.AchievementDelta(params)
	if !ok {
		return
	}
	p.board.Apply(specboard.Node{ID: u.ID, Level: u.Level, Progress: u.Progress, Fame: u.Fame}, u.HasLevel)
	p.emitSpec()
	if p.debug {
		log.Printf("[spec] delta node=%d lvl=%d hasLvl=%v", u.ID, u.Level, u.HasLevel)
	}
}

// handleSpecDone — E:152: node level-up.
func handleSpecDone(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if !p.specSelfMatches(params) {
		return
	}
	id, level, ok := capture.AchievementDone(params)
	if !ok {
		return
	}
	p.board.Complete(id, level)
	p.emitSpec()
	if p.debug {
		log.Printf("[spec] done node=%d lvl=%d", id, level)
	}
}

// emitSpec resolves node names/categories and pushes the Destiny Board DTO.
func (p *Pipeline) emitSpec() {
	rows := p.board.List()
	masteries := make([]model.MasteryLevel, len(rows))
	for i, n := range rows {
		name, category, ok := p.specNames.Resolve(n.ID)
		if !ok {
			name = "Node #" + strconv.Itoa(n.ID)
		}
		masteries[i] = model.MasteryLevel{
			Index: n.ID, Name: name, Level: n.Level,
			Progress: n.Progress, Fame: n.Fame, Category: category,
		}
	}
	count, totalFame := p.board.Totals()
	p.sink.SetSpec(model.CharacterSpec{Masteries: masteries, NodeCount: count, TotalFame: totalFame})
}
