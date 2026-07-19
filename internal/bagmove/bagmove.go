// Package bagmove is the pure detector for the phantom bag-move ghost (feature
// 023, US2). The suspected root cause is an ephemeral inventory object id being
// reused for a DIFFERENT item within a session: a stale pending drain keyed on
// that id then fabricates a ghost entry (see the comment at the objReg.Put site in
// internal/app/pipeline.go, "ids are reused across zones … would fabricate phantom
// events"). This detector observes that reuse read-only and names it, so the ghost
// becomes a deterministically replayable event instead of a live-only rumor.
//
// A count-only change is a normal restock and is NOT a bag-move — only a change of
// item index or quality on the same id counts.
package bagmove

import (
	"github.com/epaprat/albion-ledger/internal/boundedmap"
	"github.com/epaprat/albion-ledger/internal/holdings"
)

// DefaultCap bounds the session id→item map (Principle XI). Mirrors the object
// registry's order of magnitude; oldest ids evict.
const DefaultCap = 50_000

// maxRecords caps stored reuse records so a pathological capture can't grow memory;
// Observed still counts every detection even past the cap.
const maxRecords = 4096

// BagMoveRecord is one observed object-id reuse (data-model.md).
type BagMoveRecord struct {
	ObjID       int
	PriorRef    holdings.ItemRef
	NewRef      holdings.ItemRef
	FirstSeenMS int64
	ReuseMS     int64
}

// DetectorResult is the end-of-run summary; Observed==0 is a valid explicit outcome.
type DetectorResult struct {
	Records      []BagMoveRecord
	Observed     int // total reuse events detected (may exceed len(Records) past the cap)
	Declarations int // total Observe calls
}

type entry struct {
	ref         holdings.ItemRef
	firstSeenMS int64
}

// Detector tracks the item last declared for each object id within a session.
type Detector struct {
	seen         *boundedmap.Map[int, entry]
	cap          int
	records      []BagMoveRecord
	observed     int
	declarations int
}

// New returns a detector with the given id-map capacity (<=0 → DefaultCap).
func New(capacity int) *Detector {
	if capacity <= 0 {
		capacity = DefaultCap
	}
	return &Detector{seen: boundedmap.New[int, entry](capacity), cap: capacity}
}

// Observe records one object declaration. If the id already held a different item
// (by Index or Quality), it returns a *BagMoveRecord naming the reuse; otherwise nil.
func (d *Detector) Observe(objID int, newRef holdings.ItemRef, ts int64) *BagMoveRecord {
	d.declarations++
	if e, ok := d.seen.Get(objID); ok {
		if e.ref.Index != newRef.Index || e.ref.Quality != newRef.Quality {
			rec := BagMoveRecord{
				ObjID:       objID,
				PriorRef:    e.ref,
				NewRef:      newRef,
				FirstSeenMS: e.firstSeenMS,
				ReuseMS:     ts,
			}
			d.observed++
			if len(d.records) < maxRecords {
				d.records = append(d.records, rec)
			}
			d.seen.Put(objID, entry{ref: newRef, firstSeenMS: ts})
			return &rec
		}
		return nil // same item (count change is a restock, not a bag-move)
	}
	d.seen.Put(objID, entry{ref: newRef, firstSeenMS: ts})
	return nil
}

// Result returns the accumulated summary. Observed==0 means no reuse was seen — an
// explicit, honest outcome (FR-005), never silence.
func (d *Detector) Result() DetectorResult {
	return DetectorResult{
		Records:      append([]BagMoveRecord(nil), d.records...),
		Observed:     d.observed,
		Declarations: d.declarations,
	}
}

// Reset clears the session id-map and records (a new session reuses the detector).
func (d *Detector) Reset() {
	d.seen = boundedmap.New[int, entry](d.cap)
	d.records = nil
	d.observed = 0
	d.declarations = 0
}
