// Package loot correlates the player's own item-move requests with announced loot
// sources to detect ITEM loot (feature 007) — the game sends no single "you looted X"
// event. Join key everywhere is the in-world item object id. Pure domain, mutex-guarded,
// every structure capped and TTL-swept (Principle XI); expired/overflowed matches are
// counted, never silently lost (FR-004).
package loot

import "sync"

// Correlation constants (contracts/loot-correlation.md; fixed by golden tests).
const (
	sourceTTLMS    = 10 * 60 * 1000 // lootable announcements stay valid this long
	containerTTLMS = 10 * 60 * 1000
	pendingTTLMS   = 10 * 1000 // a move waits this long for its container
	dedupWindowMS  = 3 * 1000  // re-sightings of the same pickup inside this window
	sourceCap      = 4096
	containerCap   = 512
	pendingCap     = 256
	dedupCap       = 4096
)

type source struct {
	name   string
	seenMS int64
}

type container struct {
	srcObjID int
	slots    []int // slot-indexed item object ids; 0 = empty (positions preserved)
	seenMS   int64
}

type pendingMove struct {
	guid       string
	slot       int   // -1 for given-items moves
	itemObjIDs []int // given-items moves
	seenMS     int64
}

// Hit is one detected loot pickup: the item object to resolve + its source label.
type Hit struct {
	ItemObjID int
	Source    string
}

// Tracker is the loot-correlation state machine.
type Tracker struct {
	mu         sync.Mutex
	sources    map[int]*source      // lootable objId -> announcement
	srcOrder   []int                // FIFO for cap eviction
	containers map[string]*container// container GUID -> slots + source link
	ctrOrder   []string
	pending    []pendingMove
	recorded   map[int]int64 // itemObjID -> recorded-at ms (dedup)
	recOrder   []int

	droppedExpired int
	droppedCap     int
}

// New creates an empty Tracker.
func New() *Tracker {
	return &Tracker{
		sources:    map[int]*source{},
		containers: map[string]*container{},
		recorded:   map[int]int64{},
	}
}

// RegisterSource records a lootable-object announcement (NewLoot / chest events).
func (t *Tracker) RegisterSource(objID int, name string, ts int64) {
	if objID <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep(ts)
	if _, ok := t.sources[objID]; !ok {
		if len(t.sources) >= sourceCap && len(t.srcOrder) > 0 {
			old := t.srcOrder[0]
			t.srcOrder = t.srcOrder[1:]
			delete(t.sources, old)
			t.droppedCap++
		}
		t.srcOrder = append(t.srcOrder, objID)
	}
	t.sources[objID] = &source{name: name, seenMS: ts}
}

// AttachContainer records (REPLACE) a container's source link + slot map, then retries
// pending moves that were waiting for it. Returned Hits are new loot pickups.
func (t *Tracker) AttachContainer(guid string, srcObjID int, slots []int, ts int64) []Hit {
	if guid == "" {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep(ts)
	if _, ok := t.containers[guid]; !ok {
		if len(t.containers) >= containerCap && len(t.ctrOrder) > 0 {
			old := t.ctrOrder[0]
			t.ctrOrder = t.ctrOrder[1:]
			delete(t.containers, old)
			t.droppedCap++
		}
		t.ctrOrder = append(t.ctrOrder, guid)
	}
	t.containers[guid] = &container{srcObjID: srcObjID, slots: slots, seenMS: ts}

	// Retry the moves that arrived before this container was known.
	var hits []Hit
	kept := t.pending[:0]
	for _, p := range t.pending {
		if p.guid != guid {
			kept = append(kept, p)
			continue
		}
		if p.slot >= 0 {
			hits = append(hits, t.resolveSlotLocked(guid, p.slot, ts)...)
		} else {
			hits = append(hits, t.resolveIDsLocked(guid, p.itemObjIDs, ts)...)
		}
	}
	t.pending = kept
	return hits
}

// ResolveMove handles a single-item move request (op-30): src container + slot.
func (t *Tracker) ResolveMove(guid string, slot int, ts int64) []Hit {
	if guid == "" || slot < 0 {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep(ts)
	if _, known := t.containers[guid]; !known {
		t.queuePendingLocked(pendingMove{guid: guid, slot: slot, seenMS: ts})
		return nil
	}
	return t.resolveSlotLocked(guid, slot, ts)
}

// ResolveMoveGiven handles a multi-item ("take all") move request (op-39).
func (t *Tracker) ResolveMoveGiven(guid string, itemObjIDs []int, ts int64) []Hit {
	if guid == "" || len(itemObjIDs) == 0 {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep(ts)
	if _, known := t.containers[guid]; !known {
		t.queuePendingLocked(pendingMove{guid: guid, slot: -1, itemObjIDs: itemObjIDs, seenMS: ts})
		return nil
	}
	return t.resolveIDsLocked(guid, itemObjIDs, ts)
}

// Stats exposes the observable-loss counters (FR-004) and pending depth.
func (t *Tracker) Stats() (pending, droppedExpired, droppedCap int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending), t.droppedExpired, t.droppedCap
}

// ── locked internals ─────────────────────────────────────────────────────────

// resolveSlotLocked emits a Hit when guid is a loot-sourced container and the slot
// holds an unrecorded item. A known container WITHOUT a loot source is a normal
// (bank/bag) move: never a Hit, never pending (contract rule 3).
func (t *Tracker) resolveSlotLocked(guid string, slot int, ts int64) []Hit {
	c := t.containers[guid]
	src, isLoot := t.sources[c.srcObjID]
	if !isLoot {
		return nil
	}
	if slot >= len(c.slots) {
		return nil
	}
	itemObj := c.slots[slot]
	if itemObj <= 0 || t.isRecordedLocked(itemObj, ts) {
		return nil
	}
	t.recordLocked(itemObj, ts)
	return []Hit{{ItemObjID: itemObj, Source: src.name}}
}

func (t *Tracker) resolveIDsLocked(guid string, itemObjIDs []int, ts int64) []Hit {
	c := t.containers[guid]
	src, isLoot := t.sources[c.srcObjID]
	if !isLoot {
		return nil
	}
	var hits []Hit
	for _, id := range itemObjIDs {
		if id <= 0 || t.isRecordedLocked(id, ts) {
			continue
		}
		t.recordLocked(id, ts)
		hits = append(hits, Hit{ItemObjID: id, Source: src.name})
	}
	return hits
}

func (t *Tracker) queuePendingLocked(p pendingMove) {
	if len(t.pending) >= pendingCap {
		t.pending = t.pending[1:]
		t.droppedCap++
	}
	t.pending = append(t.pending, p)
}

// isRecordedLocked reports whether this item object was already counted. Recorded
// entries older than the dedup window still block re-counting (an item object is
// looted once, ever); the window matters only for map hygiene via the LRU cap.
func (t *Tracker) isRecordedLocked(itemObjID int, _ int64) bool {
	_, ok := t.recorded[itemObjID]
	return ok
}

func (t *Tracker) recordLocked(itemObjID int, ts int64) {
	if _, ok := t.recorded[itemObjID]; !ok {
		if len(t.recorded) >= dedupCap && len(t.recOrder) > 0 {
			old := t.recOrder[0]
			t.recOrder = t.recOrder[1:]
			delete(t.recorded, old)
		}
		t.recOrder = append(t.recOrder, itemObjID)
	}
	t.recorded[itemObjID] = ts
}

// sweep drops expired sources/containers/pendings (TTL). Pending expiry is counted —
// a move whose container never arrived is observable loss, not silence.
func (t *Tracker) sweep(now int64) {
	kept := t.pending[:0]
	for _, p := range t.pending {
		if now-p.seenMS > pendingTTLMS {
			t.droppedExpired++
			continue
		}
		kept = append(kept, p)
	}
	t.pending = kept

	// Source/container TTL sweeps are amortized via their FIFO order heads.
	for len(t.srcOrder) > 0 {
		id := t.srcOrder[0]
		s, ok := t.sources[id]
		if ok && now-s.seenMS <= sourceTTLMS {
			break
		}
		t.srcOrder = t.srcOrder[1:]
		if ok {
			delete(t.sources, id)
		}
	}
	for len(t.ctrOrder) > 0 {
		g := t.ctrOrder[0]
		c, ok := t.containers[g]
		if ok && now-c.seenMS <= containerTTLMS {
			break
		}
		t.ctrOrder = t.ctrOrder[1:]
		if ok {
			delete(t.containers, g)
		}
	}
}
