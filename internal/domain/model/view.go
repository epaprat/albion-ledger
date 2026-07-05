package model

// ── Item identity ────────────────────────────────────────────────────────────

// Item is a resolved game item identity.
type Item struct {
	Index       int    `json:"index"`
	DisplayName string `json:"displayName"`
	UniqueName  string `json:"uniqueName,omitempty"`
	Tier        int    `json:"tier"`    // 0 if unknown
	Enchant     int    `json:"enchant"` // 0 if none
	Quality     int    `json:"quality"` // 1-5, 0 if n/a
	Known       bool   `json:"known"`   // false → unknown-item fallback
}

// CatalogEntry is one row of the bundled/override item catalog.
type CatalogEntry struct {
	Index      int    `json:"index"`
	UniqueName string `json:"uniqueName"`
	Name       string `json:"name"`
}

// ── Valuation ────────────────────────────────────────────────────────────────

// ValuationSource is where an item's value came from.
type ValuationSource string

const (
	SourceLiveMarket     ValuationSource = "live_market"
	SourceServerEstimate ValuationSource = "server_estimate" // EMV
	// SourceExternal is a community price feed (AODP) — the lowest-priority base
	// layer; any in-game observation (live market, declared EMV) overrides it (010).
	SourceExternal ValuationSource = "external"
	SourceUnknown  ValuationSource = "unknown"
)

// DefaultStaleAfterMS is the freshness threshold (30 minutes) past which a value
// is flagged stale (FR-004; configurable).
const DefaultStaleAfterMS int64 = 30 * 60 * 1000

// Valuation is an item's estimated silver value with provenance.
type Valuation struct {
	Amount int64           `json:"amount"` // silver; 0 only when source = unknown
	Source ValuationSource `json:"source"`
	AsOf   int64           `json:"asOf"` // epoch ms of the underlying datum
	Stale  bool            `json:"stale"`
}

// ── Live view ────────────────────────────────────────────────────────────────

// LiveViewItem is one row pushed to the UI.
type LiveViewItem struct {
	Item      Item      `json:"item"`
	Valuation Valuation `json:"valuation"`
	LastSeen  int64     `json:"lastSeen"`
	Count     int       `json:"count"`
}

// CaptureStatusView is the always-visible capture status (FR-006), plus the
// drift alert (FR-012) when a patch may have shifted codes.
type CaptureStatusView struct {
	Capturing     bool    `json:"capturing"`
	Interface     string  `json:"interface"`
	GameServer    string  `json:"gameServer,omitempty"`
	EncryptedRate float64 `json:"encryptedRate"`
	Decoded       uint64  `json:"decoded"` // decoded Photon packets so far (is capture actually flowing?)
	DriftAlert    string  `json:"driftAlert,omitempty"`
}

// ── Code map (data-driven) ───────────────────────────────────────────────────

// CodeMapEntry maps a message code to a category, optionally guarded by a key.
type CodeMapEntry struct {
	Kind     string   `json:"kind"` // "event" | "response" | "request"
	Code     int      `json:"code"`
	Category Category `json:"category"`
	GuardKey *int     `json:"guardKey,omitempty"`
}
