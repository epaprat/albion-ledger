// Package store implements the local-first persistence port using SQLite via
// modernc.org/sqlite (pure-Go, no CGO). WAL mode, batched transactional writes.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/valuation"
	"github.com/epaprat/albion-ledger/internal/zonestats"
)

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  started_at INTEGER, ended_at INTEGER,
  source_kind TEXT, interface TEXT, game_server TEXT,
  decoded_count INTEGER DEFAULT 0, encrypted_count INTEGER DEFAULT 0,
  dropped_count INTEGER DEFAULT 0, unhandled_count INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS observations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT, ts INTEGER, category TEXT, message_code INTEGER,
  fields_present INTEGER, fields_expected INTEGER
);
CREATE INDEX IF NOT EXISTS idx_obs_session_cat ON observations(session_id, category, ts);
CREATE INDEX IF NOT EXISTS idx_obs_cat_ts ON observations(category, ts);
CREATE TABLE IF NOT EXISTS category_coverage (
  session_id TEXT, category TEXT,
  observed_count INTEGER, completeness REAL, encrypted_rate REAL, verdict TEXT,
  PRIMARY KEY (session_id, category)
);
CREATE TABLE IF NOT EXISTS unhandled_codes (
  session_id TEXT, kind_code TEXT, count INTEGER,
  PRIMARY KEY (session_id, kind_code)
);
CREATE TABLE IF NOT EXISTS reconciliation_notes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT, category TEXT, captured_value TEXT, ingame_value TEXT,
  result TEXT, notes TEXT, created_at INTEGER
);
CREATE TABLE IF NOT EXISTS flow_events (
  session_id TEXT, event_id TEXT, kind TEXT, ts INTEGER,
  item_index INTEGER, quality INTEGER, count INTEGER,
  silver INTEGER, fame INTEGER, valued INTEGER, source TEXT, zone TEXT,
  PRIMARY KEY (session_id, event_id)
);
CREATE INDEX IF NOT EXISTS idx_flow_session_ts ON flow_events(session_id, ts);
CREATE TABLE IF NOT EXISTS emv_book (
  item_index INTEGER, quality INTEGER, amount INTEGER, as_of INTEGER,
  PRIMARY KEY (item_index, quality)
);
`

// SQLite is a Store backed by a local SQLite database.
type SQLite struct{ db *sql.DB }

// Open opens (creating if needed) the SQLite store at path with WAL + sane PRAGMAs.
// The parent directory is created if missing so capture stores land tidily under
// captures/ (gitignored) by default.
func Open(path string) (*SQLite, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // single writer; avoids SQLITE_BUSY on bursty writes
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLite{db: db}, nil
}

// migrate applies additive schema upgrades to databases created by older builds.
// CREATE TABLE IF NOT EXISTS never alters an existing table, so any column added to
// the schema later must ALSO be added here — otherwise inserts fail on old DBs
// ("no column named ..."), silently stalling persistence (live-hit 2026-07-02: the
// flow_events table predated the zone column; every write errored and zone analytics
// read nothing).
func migrate(db *sql.DB) error {
	addColumnIfMissing := func(table, column, decl string) error {
		rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			if name == column {
				return rows.Err()
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		log.Printf("store migration: adding %s.%s", table, column)
		_, err = db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl))
		return err
	}
	return addColumnIfMissing("flow_events", "zone", `TEXT DEFAULT ''`)
}

// Close closes the database.
func (s *SQLite) Close() error { return s.db.Close() }

// StartSession inserts a new session row.
func (s *SQLite) StartSession(ctx context.Context, m model.CaptureSession) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, started_at, source_kind, interface, game_server)
		 VALUES (?, ?, ?, ?, ?)`,
		m.ID, m.StartedAt, string(m.SourceKind), m.Interface, m.GameServer)
	return err
}

// EndSession records end time + totals.
func (s *SQLite) EndSession(ctx context.Context, id string, endedAt int64, t model.SessionTotals) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET ended_at=?, decoded_count=?, encrypted_count=?,
		 dropped_count=?, unhandled_count=? WHERE id=?`,
		endedAt, t.DecodedCount, t.EncryptedCount, t.DroppedCount, t.UnhandledCount, id)
	return err
}

// AppendObservations inserts a batch within one transaction.
func (s *SQLite) AppendObservations(ctx context.Context, batch []model.Observation) error {
	if len(batch) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO observations (session_id, ts, category, message_code, fields_present, fields_expected)
		 VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, o := range batch {
		if _, err := stmt.ExecContext(ctx, o.SessionID, o.TS, string(o.Category),
			o.MessageCode, o.FieldsPresent, o.FieldsExpected); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// AppendFlowEvents inserts a batch of earnings events within one transaction. It is
// idempotent on (session_id, event_id): a re-sent event upserts, never duplicating
// (Principle VIII at-least-once + stable id; FR-008). Flow rows are the durable
// history behind the bounded in-memory ledger (Principle XI).
func (s *SQLite) AppendFlowEvents(ctx context.Context, sessionID string, batch []model.FlowEvent) error {
	if len(batch) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO flow_events
		 (session_id, event_id, kind, ts, item_index, quality, count, silver, fame, valued, source, zone)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(session_id, event_id) DO UPDATE SET
		   silver=excluded.silver, valued=excluded.valued`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, e := range batch {
		valued := 0
		if e.Valued {
			valued = 1
		}
		if _, err := stmt.ExecContext(ctx, sessionID, e.ID, string(e.Kind), e.TS,
			e.Item.Index, e.Item.Quality, e.Count, e.Silver, e.Fame, valued, e.Source, e.Zone); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// LoadFlowEvents reads flow events for zone analytics (006): ts >= sinceMS, optionally
// scoped to one capture session, ordered ts ASC, with only the columns the compute
// needs. On overflow the NEWEST `limit` rows are kept and the truncation is logged —
// never a silent cap (spec FR-006). limit <= 0 falls back to a 500k guard.
func (s *SQLite) LoadFlowEvents(ctx context.Context, sessionID string, sinceMS int64, limit int) ([]zonestats.StoredEvent, error) {
	if limit <= 0 {
		limit = 500_000
	}
	q := `SELECT session_id, kind, ts, silver, fame, count, zone FROM flow_events WHERE ts >= ?`
	args := []interface{}{sinceMS}
	if sessionID != "" {
		q += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	// DESC+LIMIT keeps the newest rows when the window exceeds the guard; reversed below.
	q += ` ORDER BY ts DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []zonestats.StoredEvent
	for rows.Next() {
		var e zonestats.StoredEvent
		var kind string
		if err := rows.Scan(&e.SessionID, &kind, &e.TS, &e.Silver, &e.Fame, &e.Count, &e.Zone); err != nil {
			return nil, err
		}
		e.Kind = model.FlowKind(kind)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == limit {
		log.Printf("flow query hit the %d-row guard — oldest rows in the window were truncated", limit)
	}
	// Reverse DESC → ASC (compute sorts internally, but keep the documented order).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// UpsertCoverage replaces the coverage rows for the session's categories.
func (s *SQLite) UpsertCoverage(ctx context.Context, rows []model.CategoryCoverage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO category_coverage
			 (session_id, category, observed_count, completeness, encrypted_rate, verdict)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(session_id, category) DO UPDATE SET
			   observed_count=excluded.observed_count, completeness=excluded.completeness,
			   encrypted_rate=excluded.encrypted_rate, verdict=excluded.verdict`,
			r.SessionID, string(r.Category), r.ObservedCount, r.Completeness,
			r.EncryptedRate, string(r.Verdict)); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// AddReconciliation stores a ground-truth note.
func (s *SQLite) AddReconciliation(ctx context.Context, n model.ReconciliationNote) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO reconciliation_notes
		 (session_id, category, captured_value, ingame_value, result, notes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		n.SessionID, string(n.Category), n.CapturedValue, n.IngameValue, n.Result, n.Notes, n.CreatedAt)
	return err
}

// SaveUnhandled persists the unhandled-code histogram for code discovery.
func (s *SQLite) SaveUnhandled(ctx context.Context, sessionID string, hist map[string]int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for kc, n := range hist {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO unhandled_codes (session_id, kind_code, count) VALUES (?, ?, ?)
			 ON CONFLICT(session_id, kind_code) DO UPDATE SET count=excluded.count`,
			sessionID, kc, n); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// LoadSession returns a session's metadata and totals (concrete helper for the CLI).
func (s *SQLite) LoadSession(ctx context.Context, id string) (model.CaptureSession, model.SessionTotals, error) {
	var m model.CaptureSession
	var t model.SessionTotals
	var kind string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, started_at, ended_at, source_kind, interface, game_server,
		 decoded_count, encrypted_count, dropped_count, unhandled_count
		 FROM sessions WHERE id=?`, id).
		Scan(&m.ID, &m.StartedAt, &m.EndedAt, &kind, &m.Interface, &m.GameServer,
			&t.DecodedCount, &t.EncryptedCount, &t.DroppedCount, &t.UnhandledCount)
	m.SourceKind = model.SourceKind(kind)
	return m, t, err
}

// LoadCoverage returns the coverage rows for a session.
func (s *SQLite) LoadCoverage(ctx context.Context, sessionID string) ([]model.CategoryCoverage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT category, observed_count, completeness, encrypted_rate, verdict
		 FROM category_coverage WHERE session_id=? ORDER BY category`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.CategoryCoverage
	for rows.Next() {
		var c model.CategoryCoverage
		var cat, verdict string
		if err := rows.Scan(&cat, &c.ObservedCount, &c.Completeness, &c.EncryptedRate, &verdict); err != nil {
			return nil, err
		}
		c.SessionID, c.Category, c.Verdict = sessionID, model.Category(cat), model.Verdict(verdict)
		out = append(out, c)
	}
	return out, rows.Err()
}

// LoadReconciliations returns the reconciliation notes for a session.
func (s *SQLite) LoadReconciliations(ctx context.Context, sessionID string) ([]model.ReconciliationNote, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, category, captured_value, ingame_value, result, notes, created_at
		 FROM reconciliation_notes WHERE session_id=? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ReconciliationNote
	for rows.Next() {
		var n model.ReconciliationNote
		var cat string
		if err := rows.Scan(&n.ID, &cat, &n.CapturedValue, &n.IngameValue, &n.Result, &n.Notes, &n.CreatedAt); err != nil {
			return nil, err
		}
		n.SessionID, n.Category = sessionID, model.Category(cat)
		out = append(out, n)
	}
	return out, rows.Err()
}

// SaveEMVBook upserts the whole server-estimate book in one transaction (010: the
// book survives restarts so values learned from declarations keep pricing the K
// bank-overview summary rows in later sessions). Newest as_of wins.
func (s *SQLite) SaveEMVBook(ctx context.Context, entries []valuation.EMVEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO emv_book (item_index, quality, amount, as_of)
		VALUES (?,?,?,?)
		ON CONFLICT(item_index, quality) DO UPDATE SET
		  amount=excluded.amount, as_of=excluded.as_of
		WHERE excluded.as_of >= emv_book.as_of`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		if _, err := stmt.ExecContext(ctx, e.Index, e.Quality, e.Amount, e.AsOf); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LoadEMVBook reads all persisted server estimates.
func (s *SQLite) LoadEMVBook(ctx context.Context) ([]valuation.EMVEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_index, quality, amount, as_of FROM emv_book`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []valuation.EMVEntry
	for rows.Next() {
		var e valuation.EMVEntry
		if err := rows.Scan(&e.Index, &e.Quality, &e.Amount, &e.AsOf); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
