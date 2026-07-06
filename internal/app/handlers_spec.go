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

// maxSpecUnlocked bounds the persisted unlocked/maxed set (Principle XI). The real
// Destiny Board is ~697 nodes; anything past the extractor's 2048 cap is hostile.
const maxSpecUnlocked = 2048

func init() {
	register(model.CatSpecSnapshot, handleSpecSnapshot)
	register(model.CatSpecDelta, handleSpecDelta)
	register(model.CatSpecDone, handleSpecDone)
	register(model.CatSpecUnlocked, handleSpecUnlocked)
	register(model.CatSpecFullBoard, handleSpecFullBoard)
}

// handleSpecFullBoard — E:1: the COMPLETE board authority (012). k2 (node ids) + k3
// (levels incl 100) decode directly to id→level pairs, so every maxed node shows at
// login with zero grind. A cold login carries k2 (persisted as the enumeration); a
// warm login omits it and its k3 is mapped through the learned enumeration. Rejected
// if it doesn't match the E:1 board shape (the chat-settings E:1 shares event code 1).
func handleSpecFullBoard(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if !p.specSelfMatches(params) {
		return
	}
	fb, ok := capture.AchievementFullBoard(params)
	if !ok {
		return
	}
	pairs := p.specEnum.Decode(fb.Ids, fb.Levels)
	if len(pairs) == 0 {
		return // warm login before any enum is known → 011 fallback stays in charge
	}
	// Install only TOUCHED nodes (level > 0). E:1 enumerates the WHOLE board — hundreds
	// of level-0 nodes with no fame — and installing those would inflate NodeCount to the
	// full board size and (since E:154 now merges, never clears) keep it inflated all
	// session, breaking the touched-only NodeCount/TotalFame invariant. Untouched nodes
	// still render from the catalog at level 0 in emitSpec; maxed (level 100) install here.
	nodes := make([]specboard.Node, 0, len(pairs))
	for _, pr := range pairs {
		if pr.Level <= 0 {
			continue
		}
		n := specboard.Node{ID: pr.ID, Level: pr.Level}
		if pr.Level >= 100 {
			n.Progress = 1
		}
		nodes = append(nodes, n)
	}
	p.board.ReplaceAll(nodes) // E:1 is authoritative and complete — it REPLACES.
	p.specFullBoard = true
	p.specSnapshotSeen = true
	p.specReplacePending = false // E:1 already replaced; the next E:154 merges onto it.
	if fb.IdsFromWire {
		p.sink.SetSpecEnum(p.specEnum.Snapshot()) // persist the learned id order
	}
	p.emitSpec()
	if p.debug {
		maxed := 0
		for _, pr := range pairs {
			if pr.Level >= 100 {
				maxed++
			}
		}
		log.Printf("[spec] E:1 board n=%d maxed=%d k2FromWire=%v", len(pairs), maxed, fb.IdsFromWire)
	}
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
	// MERGE, never replace: E:155 is an INCOMPLETE unlocked list — long-idle maxed
	// nodes (zero elite fame) are absent from it (Cultist Robe, live-proven). The
	// seed + noteMaxed accumulate those; a wholesale replace here would delete them
	// and the shutdown SaveSpecUnlocked would lose them permanently. Union only.
	if p.specUnlocked == nil {
		p.specUnlocked = make(map[int]bool, len(ids))
	}
	changed := false
	for _, id := range ids {
		if !p.specUnlocked[id] && len(p.specUnlocked) < maxSpecUnlocked {
			p.specUnlocked[id] = true
			changed = true
		}
	}
	p.specUnlockedSeen = true
	if changed {
		all := make([]int, 0, len(p.specUnlocked))
		for n := range p.specUnlocked {
			all = append(all, n)
		}
		p.sink.SetSpecUnlocked(all) // persist the accumulated union
	}
	p.emitSpec()
	if p.debug {
		log.Printf("[spec] unlocked (E:155) n=%d", len(ids))
	}
}

// specSelfMatches gates all three messages on k0 == self (005 isSelfObj pattern).
func (p *Pipeline) specSelfMatches(params map[byte]interface{}) bool {
	self, ok := capture.SpecSelf(params)
	if !ok {
		return false
	}
	// The achievement stream is the local client's OWN board — the server never sends
	// another player's. Before the first Join sets selfObjID, trust it (else the
	// login-burst E:154, which can precede op-2, would be dropped and the panel would
	// stay empty until the next zone change). Once self is known, filter strictly.
	return p.selfObjID == 0 || self == p.selfObjID
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
	// Once E:1 has given the complete board (incl maxed), E:154 (in-progress only)
	// must MERGE — it adds progress/fame to in-progress nodes but must never wipe the
	// maxed E:1 established (a ReplaceAll would drop them).
	if p.specFullBoard {
		p.board.MergeAll(nodes)
	} else if p.specReplacePending {
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
	p.board.Apply(specboard.Node{ID: u.ID, Level: u.Level, Progress: u.Progress, Fame: u.Fame}, u.HasLevel, u.HasProgress, u.HasFame)
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
	// Bounded (XI): the real board is ~697 nodes; refuse growth past the board cap so
	// a hostile level-100 E:153/E:152 stream can't grow the map — or the full-table
	// spec_unlocked rewrite it triggers — without bound.
	if len(p.specUnlocked) >= maxSpecUnlocked {
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
	add := func(c model.SpecNodeCatalog, n specboard.Node, touched bool) {
		masteries = append(masteries, model.MasteryLevel{
			Index: c.ID, Name: c.Name, Level: n.Level, Progress: n.Progress, Fame: n.Fame,
			Category: c.Category, Subcategory: c.Subcategory, Slot: c.Slot, Base: c.Base, Touched: touched, FameToMax: c.FameToMax,
		})
	}
	for _, c := range catalog {
		inCatalog[c.ID] = true
		if n, inProg := prog[c.ID]; inProg {
			add(c, n, n.Level > 0 || n.Fame > 0)
		} else if p.specUnlocked[c.ID] && p.specSnapshotSeen {
			// Unlocked but not in the CURRENT in-progress snapshot → maxed. The
			// snapshot is REQUIRED: the unlocked set alone (e.g. the persisted seed
			// at startup, before any zone join) contains in-progress nodes too, and
			// classifying against an empty board marked EVERYTHING level 100.
			add(c, specboard.Node{Level: 100, Progress: 1}, true)
		} else {
			add(c, specboard.Node{}, false)
		}
	}
	// A live node not in the catalog (unknown id) still shows, honestly labelled.
	for id, n := range prog {
		if !inCatalog[id] {
			add(model.SpecNodeCatalog{ID: id, Name: "Node #" + strconv.Itoa(id), Category: "Other"}, n, true)
		}
	}
	count, totalFame := p.board.Totals()
	p.sink.SetSpec(model.CharacterSpec{
		Masteries: masteries, NodeCount: count, TotalFame: totalFame,
		Complete: p.specFullBoard || (p.specUnlockedSeen && p.specSnapshotSeen),
	})
}
