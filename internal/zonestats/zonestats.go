// Package zonestats turns the accumulated flow-event history into per-zone earning
// rates ("nerede kasayım" — feature 006): for each zone, totals per activity kind and
// per-hour rates over a GAP-CAPPED active time derived from event timestamps. Pure
// domain — no infrastructure imports (Principle I); deterministic and golden-testable.
package zonestats

import (
	"sort"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

// Active-time derivation constants (research Decision 1; fixed by golden tests).
const (
	// GapCapMS caps how much of the gap between two consecutive events counts as
	// active time — anything longer is idle/travel and must not dilute the rate.
	GapCapMS = 5 * 60 * 1000
	// TailMS is the activity tail credited after the last event of each chain
	// (a single-event zone earns exactly TailMS).
	TailMS = 30 * 1000
	// RateMinMS is the minimum active time before a per-hour rate is meaningful;
	// below it the zone is flagged InsufficientData and rates stay 0.
	RateMinMS = 60 * 1000
)

// StoredEvent is a flow event as read back from the store, carrying the capture
// session it belongs to: chains never span sessions (the gap between two app runs
// is not activity, whatever its length).
type StoredEvent struct {
	model.FlowEvent
	SessionID string
}

// ActivityStat is one activity-kind slice of a zone's rollup. PerHour uses the
// ZONE's active time as denominator — concurrent activities share one time pool.
type ActivityStat struct {
	Kind       model.FlowKind
	Total      int64 // silver-denominated; fame amount for kind=fame
	PerHour    int64
	EventCount int
}

// ZoneStat is one zone's earnings rollup for the queried window.
type ZoneStat struct {
	Zone             string // "" = unlabeled (caller renders "Unknown zone")
	ActiveMS         int64
	NetSilver        int64
	SilverPerHour    int64
	GatherValue      int64
	GatherPerHour    int64
	Fame             int64
	FamePerHour      int64
	EventCount       int
	InsufficientData bool
	Activities       []ActivityStat
}

// Compute groups events by zone, derives gap-capped active time per zone (chains
// broken at session boundaries), and returns rollups sorted by SilverPerHour desc
// (ties: NetSilver desc, then zone name for stability). Input need not be sorted.
func Compute(events []StoredEvent) []ZoneStat {
	byZone := map[string][]StoredEvent{}
	for _, e := range events {
		byZone[e.Zone] = append(byZone[e.Zone], e)
	}

	out := make([]ZoneStat, 0, len(byZone))
	for zone, evs := range byZone {
		out = append(out, computeZone(zone, evs))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SilverPerHour != out[j].SilverPerHour {
			return out[i].SilverPerHour > out[j].SilverPerHour
		}
		if out[i].NetSilver != out[j].NetSilver {
			return out[i].NetSilver > out[j].NetSilver
		}
		return out[i].Zone < out[j].Zone
	})
	return out
}

func computeZone(zone string, evs []StoredEvent) ZoneStat {
	sort.Slice(evs, func(i, j int) bool {
		if evs[i].TS != evs[j].TS {
			return evs[i].TS < evs[j].TS
		}
		return evs[i].SessionID < evs[j].SessionID // stable order for equal timestamps
	})

	// Gap-capped active time: Σ min(gap, GapCapMS) within a session + TailMS per chain.
	// A chain ends where the session changes; the cross-session gap never counts.
	var active int64
	for i, e := range evs {
		if i > 0 && evs[i-1].SessionID == e.SessionID {
			gap := e.TS - evs[i-1].TS
			if gap > GapCapMS {
				gap = GapCapMS
			}
			if gap > 0 {
				active += gap
			}
			continue
		}
		if i > 0 {
			active += TailMS // close the previous session's chain
		}
	}
	if len(evs) > 0 {
		active += TailMS // close the final chain
	}

	st := ZoneStat{Zone: zone, ActiveMS: active, EventCount: len(evs)}
	kinds := map[model.FlowKind]*ActivityStat{}
	for _, e := range evs {
		k := kinds[e.Kind]
		if k == nil {
			k = &ActivityStat{Kind: e.Kind}
			kinds[e.Kind] = k
		}
		k.EventCount++
		if e.Kind == model.FlowFame {
			k.Total += e.Fame
			st.Fame += e.Fame
			continue
		}
		k.Total += e.Silver
		st.NetSilver += e.Silver
		if e.Kind == model.FlowGather {
			st.GatherValue += e.Silver
		}
	}

	st.InsufficientData = active < RateMinMS
	if !st.InsufficientData {
		st.SilverPerHour = perHour(st.NetSilver, active)
		st.GatherPerHour = perHour(st.GatherValue, active)
		st.FamePerHour = perHour(st.Fame, active)
	}

	st.Activities = make([]ActivityStat, 0, len(kinds))
	for _, k := range kinds {
		if !st.InsufficientData {
			k.PerHour = perHour(k.Total, active)
		}
		st.Activities = append(st.Activities, *k)
	}
	sort.Slice(st.Activities, func(i, j int) bool {
		if st.Activities[i].Total != st.Activities[j].Total {
			return st.Activities[i].Total > st.Activities[j].Total
		}
		return st.Activities[i].Kind < st.Activities[j].Kind
	})
	return st
}

func perHour(total, activeMS int64) int64 {
	if activeMS <= 0 {
		return 0
	}
	return total * 3_600_000 / activeMS
}
