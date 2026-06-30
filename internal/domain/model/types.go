package model

// Verdict is the measured confidence for a category in a session.
type Verdict string

const (
	VerdictNotObserved Verdict = "not_observed"
	VerdictLow         Verdict = "low"
	VerdictMedium      Verdict = "medium"
	VerdictHigh        Verdict = "high"
)

// SourceKind distinguishes live capture from deterministic replay.
type SourceKind string

const (
	SourceLive   SourceKind = "live"
	SourceReplay SourceKind = "replay"
)

// CaptureSession is one probe run.
type CaptureSession struct {
	ID         string
	StartedAt  int64 // epoch ms
	EndedAt    int64 // 0 while running
	SourceKind SourceKind
	Interface  string // capture interface (live) or recording path (replay)
	GameServer string // detected, may be empty
}

// SessionTotals are the rolled-up counters for a finished session.
type SessionTotals struct {
	DecodedCount   uint64
	EncryptedCount uint64
	DroppedCount   uint64
	UnhandledCount uint64
}

// EncryptedRate returns encrypted / (decoded + encrypted), 0 when no traffic.
func (t SessionTotals) EncryptedRate() float64 {
	denom := t.DecodedCount + t.EncryptedCount
	if denom == 0 {
		return 0
	}
	return float64(t.EncryptedCount) / float64(denom)
}

// Observation is one decoded message attributed to a category.
type Observation struct {
	SessionID      string
	TS             int64 // epoch ms
	Category       Category
	MessageCode    int
	FieldsPresent  int
	FieldsExpected int
}

// CategoryCoverage is a per-session per-category rollup.
type CategoryCoverage struct {
	SessionID     string
	Category      Category
	ObservedCount int
	Completeness  float64 // 0..1
	EncryptedRate float64 // 0..1
	Verdict       Verdict
}

// ReconciliationNote is a maintainer-recorded ground-truth check.
type ReconciliationNote struct {
	ID            int64
	SessionID     string
	Category      Category
	CapturedValue string
	IngameValue   string
	Result        string // "pass" | "fail"
	Notes         string
	CreatedAt     int64
}

// CaptureStatus is the always-visible capture state (Principle VII).
type CaptureStatus struct {
	Capturing     bool
	Interface     string
	GameServer    string
	Decoded       uint64
	EncryptedRate float64
}
