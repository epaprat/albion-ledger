package app

// ── Ordering contract (FR-006, 009) ──────────────────────────────────────────
// Handlers run on the single capture goroutine; where one wire message feeds several
// consumers, the order is EXPLICIT here — never implied by file or case position:
//  1. Response path: key-253==2 → updateSelf runs BEFORE dispatch (OnResponse) — self
//     identity/zone/bridge must be fresh for every handler that attributes earnings.
//  2. Event path: registerNewItem (declarations 30-37) runs BEFORE dispatch (OnEvent)
//     — containers referencing the new object ids must resolve in the same packet.
//  3. loot_move: loot correlation resolves BEFORE the holdings move-application, both
//     inside ONE handler body (handleLootMove) — two registrations with implicit
//     ordering are forbidden.
//  4. Declaration drains inside registerNewItem: objReg write → pendingInv →
//     pendingLootResolve → pendingPuts (the pre-009 order, pinned by golden tests).

import (
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

// Handler processes one classified message for its category.
type Handler func(p *Pipeline, kind probe.Kind, code int, params map[byte]interface{})

// registry maps a category to its single handler. Filled by the register calls in
// the handlers_*.go files at package init; read-only afterwards. Adding a category
// touches only its own file — this file never changes for new categories (SC-002).
var registry = map[model.Category]Handler{}

// register wires a category to its handler; a duplicate registration is a programmer
// error and must fail loudly at startup, not silently shadow an existing handler.
func register(cat model.Category, h Handler) {
	if _, dup := registry[cat]; dup {
		panic("app: duplicate handler registration for category " + string(cat))
	}
	registry[cat] = h
}

// dispatch classifies a message and routes it to its category handler.
func (p *Pipeline) dispatch(kind probe.Kind, code int, params map[byte]interface{}) {
	cl, ok := p.clf.Classify(kind, code, params)
	if !ok {
		return
	}
	if h, ok := registry[cl.Category]; ok {
		h(p, kind, code, params)
	}
}
