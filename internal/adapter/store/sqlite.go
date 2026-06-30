// Package store implements the local-first persistence port using SQLite via
// modernc.org/sqlite (pure-Go, no CGO). WAL mode, batched transactional writes.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/epaprat/albion-ledger/internal/domain/model"
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
	return &SQLite{db: db}, nil
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
