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
	register(model.CatSpecUnlocked, handleSpecUnlocked)
}

// handleSpecUnlocked — E:155: the full unlocked-node list (in-progress ∪ maxed). Any
// id here that is NOT in the current in-progress snapshot (E:154) is a maxed node
// (level 100). The classification happens at emit time, so E:154/E:155 order in the
// login burst doesn't matter — board membership always wins.
func handleSpecUnlocked(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	// E:155 is the client's OWN unlocked list — the server never sends another
	// player's, so we do NOT require k0==currentSelf (which can be briefly stale
	// right after a zone change and would drop this rare, important event).
	if _, ok := capture.SpecSelf(params); !ok {
		return
	}
	ids := capture.SpecUnlockedIDs(params)
	if len(ids) == 0 {
		return
	}
	p.specUnlocked = make(map[int]bool, len(ids))
	for _, id := range ids {
		p.specUnlocked[id] = true
	}
	p.specUnlockedSeen = true
	p.sink.SetSpecUnlocked(ids) // persist so maxed branches survive restarts
	p.emitSpec()
	if p.debug {
		log.Printf("[spec] unlocked (E:155) n=%d", len(ids))
	}
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
	p.specSnapshotSeen = true
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
	if u.HasLevel && u.Level >= 100 {
		p.noteMaxed(u.ID)
	}
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
	if level >= 100 {
		p.noteMaxed(id)
	}
	p.emitSpec()
	if p.debug {
		log.Printf("[spec] done node=%d lvl=%d", id, level)
	}
}

// noteMaxed adds a node observed at level >=100 (E:152/E:153) to the persistent
// unlocked set — long-idle maxed nodes never appear in E:154/E:155 (live-proven:
// a 100/100 robe with zero elite fame is absent from BOTH), so any elite tick or
// completion is captured permanently the moment it happens.
func (p *Pipeline) noteMaxed(id int) {
	if p.specUnlocked == nil {
		p.specUnlocked = map[int]bool{}
	}
	if p.specUnlocked[id] {
		return
	}
	p.specUnlocked[id] = true
	ids := make([]int, 0, len(p.specUnlocked))
	for n := range p.specUnlocked {
		ids = append(ids, n)
	}
	p.sink.SetSpecUnlocked(ids)
}

// emitSpec builds the COMPLETE Destiny Board (every catalog node, untouched ones at
// level 0 like the in-game B menu) with the player's live progress overlaid, and
// pushes the DTO. NodeCount/TotalFame stay TOUCHED-only (real progress totals).
func (p *Pipeline) emitSpec() {
	// Live progress by node id.
	prog := map[int]specboard.Node{}
	for _, n := range p.board.List() {
		prog[n.ID] = n
	}
	catalog := p.specNames.All()
	masteries := make([]model.MasteryLevel, 0, len(catalog)+len(prog))
	inCatalog := make(map[int]bool, len(catalog))
	add := func(id int, name, category, subcategory string, n specboard.Node, touched bool, fameToMax int64) {
		masteries = append(masteries, model.MasteryLevel{
			Index: id, Name: name, Level: n.Level, Progress: n.Progress, Fame: n.Fame,
			Category: category, Subcategory: subcategory, Touched: touched, FameToMax: fameToMax,
		})
	}
	for _, c := range catalog {
		inCatalog[c.ID] = true
		if n, inProg := prog[c.ID]; inProg {
			add(c.ID, c.Name, c.Category, c.Subcategory, n, n.Level > 0 || n.Fame > 0, c.FameToMax)
		} else if p.specUnlocked[c.ID] && p.specSnapshotSeen {
			// Unlocked but not in the CURRENT in-progress snapshot → maxed. The
			// snapshot is REQUIRED: the unlocked set alone (e.g. the persisted seed
			// at startup, before any zone join) contains in-progress nodes too, and
			// classifying against an empty board marked EVERYTHING level 100.
			add(c.ID, c.Name, c.Category, c.Subcategory, specboard.Node{Level: 100, Progress: 1}, true, c.FameToMax)
		} else {
			add(c.ID, c.Name, c.Category, c.Subcategory, specboard.Node{}, false, c.FameToMax)
		}
	}
	// A live node not in the catalog (unknown id) still shows, honestly labelled.
	for id, n := range prog {
		if !inCatalog[id] {
			add(id, "Node #"+strconv.Itoa(id), "Other", "", n, true, 0)
		}
	}
	count, totalFame := p.board.Totals()
	p.sink.SetSpec(model.CharacterSpec{
		Masteries: masteries, NodeCount: count, TotalFame: totalFame,
		Complete: p.specUnlockedSeen && p.specSnapshotSeen,
	})
}
