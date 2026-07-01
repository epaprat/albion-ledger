package model

// FlowKind is the type of an earnings (flow) event.
type FlowKind string

const (
	FlowSilver FlowKind = "silver"
	FlowLoot   FlowKind = "loot"
	FlowGather FlowKind = "gather"
	FlowFame   FlowKind = "fame"
)

// FlowEvent is one player earnings event (silver picked up, item looted/gathered,
// or fame gained). Silver/fame carry an amount; loot/gather carry a resolved item.
type FlowEvent struct {
	ID     string   `json:"id"`     // stable dedup id (retry/echo → one event, FR-008)
	Kind   FlowKind `json:"kind"`   // silver | loot | gather | fame
	TS     int64    `json:"ts"`     // epoch ms of the event
	Item   Item     `json:"item"`   // loot/gather resolved item; zero for silver/fame
	Count  int      `json:"count"`  // loot/gather stack count; 1 for silver/fame
	Silver int64    `json:"silver"` // silver-kind: net silver; loot/gather: value×count; fame: 0
	Fame   int64    `json:"fame"`   // fame-kind amount; 0 otherwise
	Valued bool     `json:"valued"` // loot/gather: value known (false → "unvalued", FR-004)
	Source string   `json:"source"` // best-effort label (mob/node/etc.)
	Zone   string   `json:"zone"`   // cluster id / zone label where it happened (per-zone analytics, 006)
}

// SessionSummary is the derived rollup for the current activity session (AFM-style:
// lazy start on first earning, idle auto-close, pause-aware elapsed).
type SessionSummary struct {
	SelfKnown     bool  `json:"selfKnown"`     // local player identified yet (needs a Join/zone) — else silver/loot/gather can't be attributed
	Active        bool  `json:"active"`        // session open (false after idle auto-close)
	StartedMS     int64 `json:"startedMs"`     // first-earning time (0 = no session yet)
	ElapsedMS     int64 `json:"elapsedMs"`     // pause-aware active elapsed
	NetSilver     int64 `json:"netSilver"`     // Σ silver-net + loot value + gather value
	SilverPerHour int64 `json:"silverPerHour"` // NetSilver / activeHours (0 until RateReady)
	LootValue     int64 `json:"lootValue"`     // Σ loot value
	GatherValue   int64 `json:"gatherValue"`   // Σ gather value
	Fame          int64 `json:"fame"`          // Σ fame (separate; never in silver, SC-005)
	FamePerHour   int64 `json:"famePerHour"`   // Fame / activeHours (0 until RateReady)
	RateReady     bool  `json:"rateReady"`     // elapsed ≥ 60s (SC-006)
	UnvaluedCount int   `json:"unvaluedCount"` // loot/gather items with unknown value
	EventCount    int   `json:"eventCount"`    // total flow events this session
}

// FlowItemStatView is one row of the per-item loot/gather breakdown (AFM-style):
// total quantity this session with the current per-item + stack (total) value.
type FlowItemStatView struct {
	Kind            FlowKind `json:"kind"`
	ItemDisplayName string   `json:"itemDisplayName"`
	UniqueName      string   `json:"uniqueName,omitempty"`
	Tier            int      `json:"tier"`
	Enchant         int      `json:"enchant"`
	Quality         int      `json:"quality"`
	Qty             int      `json:"qty"`
	UnitValue       int64    `json:"unitValue"`  // per-item EMV
	TotalValue      int64    `json:"totalValue"` // stack EMV = unit × qty
	Valued          bool     `json:"valued"`
	LastSeen        int64    `json:"lastSeen"`
}

// FlowEventView is the UI-facing flow row (flat item fields; no domain Item leak).
type FlowEventView struct {
	Kind            FlowKind `json:"kind"`
	TS              int64    `json:"ts"`
	ItemDisplayName string   `json:"itemDisplayName,omitempty"`
	UniqueName      string   `json:"uniqueName,omitempty"`
	Tier            int      `json:"tier"`
	Enchant         int      `json:"enchant"`
	Quality         int      `json:"quality"`
	Count           int      `json:"count"`
	Silver          int64    `json:"silver"`
	Fame            int64    `json:"fame"`
	Valued          bool     `json:"valued"`
	Source          string   `json:"source,omitempty"`
	Zone            string   `json:"zone,omitempty"`
}
