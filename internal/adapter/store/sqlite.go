// Package store implements the local-first persistence port using SQLite via
// modernc.org/sqlite (pure-Go, no CGO). WAL mode, batched transactional writes.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/flow"
	"github.com/epaprat/albion-ledger/internal/holdings"
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
CREATE TABLE IF NOT EXISTS spec_unlocked (
  node_id INTEGER PRIMARY KEY
);
CREATE TABLE IF NOT EXISTS spec_enum (
  pos INTEGER PRIMARY KEY, node_id INTEGER
);
CREATE TABLE IF NOT EXISTS mail_infos (
  mail_id INTEGER PRIMARY KEY,
  type TEXT, location_id TEXT, received INTEGER
);
CREATE TABLE IF NOT EXISTS trades (
  trade_id TEXT PRIMARY KEY,
  direction TEXT, source TEXT, item_id TEXT, item_index INTEGER,
  partial_amount INTEGER, total_amount INTEGER,
  gross INTEGER, setup_fee INTEGER, sales_tax INTEGER, net INTEGER,
  tax_estimated INTEGER, unit_silver REAL,
  received INTEGER, location_id TEXT
);
CREATE TABLE IF NOT EXISTS holdings_containers (
  container_id TEXT PRIMARY KEY,
  location TEXT, city TEXT, tab TEXT,
  items_json TEXT, last_seen INTEGER, pinned INTEGER,
  summary INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS wallet_state (
  id INTEGER PRIMARY KEY,
  silver INTEGER, last_seen INTEGER
);
CREATE TABLE IF NOT EXISTS spec_board (
  id INTEGER PRIMARY KEY,
  board_json TEXT, last_seen INTEGER
);
CREATE TABLE IF NOT EXISTS flow_checkpoint (
  id INTEGER PRIMARY KEY,
  payload_json TEXT, last_activity INTEGER
);
CREATE TABLE IF NOT EXISTS flow_sessions (
  session_id INTEGER PRIMARY KEY AUTOINCREMENT,
  started_ms INTEGER, ended_ms INTEGER, active_elapsed_ms INTEGER,
  net_silver INTEGER, loot_value INTEGER, gather_value INTEGER, fame INTEGER,
  silver_per_hour INTEGER, items_json TEXT
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
	if err := addColumnIfMissing("flow_events", "zone", `TEXT DEFAULT ''`); err != nil {
		return err
	}
	// The K-summary flag is persisted (020 live-test fix): without it, a hydrated summary
	// loaded as a pseudo-physical and the read-time dedup could not skip it, double-counting
	// its live physical peer (247 = 119 summary + 128 physical). Backfill existing DBs: every
	// persisted bank container is a summary (Snapshot() never persists a physical open), and
	// the vault: prefix is the summary stable-id convention.
	if err := addColumnIfMissing("holdings_containers", "summary", `INTEGER DEFAULT 0`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE holdings_containers SET summary=1 WHERE container_id LIKE 'vault:%'`); err != nil {
		return err
	}
	// Ephemeral physical bank containers (keyed by a per-open wire guid) must never have been
	// persisted (020 fix): they never recur, so they hydrate as stale junk that wrongly wins
	// the read-time dedup over a fresh K summary. Purge any a buggy build wrote — only the
	// stable-id K summaries (vault:*) and the self bag/equipped (self-*) are legitimate.
	if _, err := db.Exec(`DELETE FROM holdings_containers WHERE container_id NOT LIKE 'vault:%' AND container_id NOT LIKE 'self-%'`); err != nil {
		return err
	}
	// The trades table gained a full fee/tax/net breakdown (017 expansion): the original
	// mail-only shape (mail_id PK, total_silver) is incompatible. The feature is
	// unreleased, so a pre-expansion table is dropped and recreated by the schema on the
	// next Open — no user data is at stake.
	var hasMailID int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('trades') WHERE name='mail_id'`).Scan(&hasMailID); err != nil {
		return err
	}
	if hasMailID > 0 {
		log.Printf("store migration: dropping pre-expansion trades table")
		if _, err := db.Exec(`DROP TABLE trades`); err != nil {
			return err
		}
		if _, err := db.Exec(schema); err != nil { // recreate all IF-NOT-EXISTS tables incl. new trades
			return err
		}
	}
	return nil
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

// SaveSpecUnlocked persists the Destiny Board unlocked-node set (E:155): the full
// list so maxed (level-100) branches survive restarts, since E:155 only arrives on
// node completion, not at login (011, live-confirmed 2026-07-05). REPLACE semantics.
func (s *SQLite) SaveSpecUnlocked(ctx context.Context, ids []int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM spec_unlocked`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO spec_unlocked(node_id) VALUES (?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LoadSpecUnlocked returns the persisted unlocked-node ids.
func (s *SQLite) LoadSpecUnlocked(ctx context.Context) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id FROM spec_unlocked`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SaveSpecEnum persists the E:1 board enumeration (position→node id, 012) so a warm
// login whose E:1 omits the id list still decodes. REPLACE semantics (a fresh cold
// login re-sends the full order).
func (s *SQLite) SaveSpecEnum(ctx context.Context, ids []int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM spec_enum`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO spec_enum(pos, node_id) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for pos, id := range ids {
		if _, err := stmt.ExecContext(ctx, pos, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LoadSpecEnum returns the persisted enumeration as an id slice ordered by position.
func (s *SQLite) LoadSpecEnum(ctx context.Context) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id FROM spec_enum ORDER BY pos`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// tradesCap bounds the persisted trade ledger (feature 017): the newest tradesCap mails
// are kept, the rest pruned oldest-first so a long-lived mailbox can't grow the store
// without bound (Principle XI). Ordered by received then mail_id (mail ids increase over
// time, so they order stably even when received is unknown/0).
const tradesCap = 5000

// SaveTrade upserts one captured trade by its id (idempotent — the same mail/instant
// trade must not double-count, FR-010), then prunes beyond tradesCap.
func (s *SQLite) SaveTrade(ctx context.Context, t model.Trade) error {
	est := 0
	if t.TaxEstimated {
		est = 1
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO trades
		  (trade_id, direction, source, item_id, item_index, partial_amount, total_amount,
		   gross, setup_fee, sales_tax, net, tax_estimated, unit_silver, received, location_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TradeID, t.Direction, t.Source, t.ItemID, t.ItemIndex, t.PartialAmount, t.TotalAmount,
		t.Gross, t.SetupFee, t.SalesTax, t.Net, est, t.UnitSilver, t.Received, t.LocationID,
	); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM trades WHERE trade_id NOT IN (
		  SELECT trade_id FROM trades ORDER BY received DESC, trade_id DESC LIMIT ?)`, tradesCap)
	return err
}

// mailInfosCap bounds the persisted mail-type map (Principle XI); a mailbox has at most
// a few hundred mails, so this is generous.
const mailInfosCap = 8192

// SaveMailInfo persists one mail's type/location so a mail read in a later session (whose
// GetMailInfos list the game client-cached and never re-sent) can still be decoded — the
// type is what a ReadMail body needs to parse (017). Prunes beyond the cap.
func (s *SQLite) SaveMailInfo(ctx context.Context, id int64, typ, location string, received int64) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO mail_infos (mail_id, type, location_id, received) VALUES (?, ?, ?, ?)`,
		id, typ, location, received); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM mail_infos WHERE mail_id NOT IN (
		  SELECT mail_id FROM mail_infos ORDER BY received DESC, mail_id DESC LIMIT ?)`, mailInfosCap)
	return err
}

// LoadMailInfos returns the persisted mail-type map (for the startup seed).
func (s *SQLite) LoadMailInfos(ctx context.Context) ([]model.MailInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT mail_id, type, location_id, received FROM mail_infos`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.MailInfo
	for rows.Next() {
		var m model.MailInfo
		if err := rows.Scan(&m.ID, &m.Type, &m.LocationID, &m.Received); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// LoadTrades returns every persisted trade, newest first (received then trade_id).
func (s *SQLite) LoadTrades(ctx context.Context) ([]model.Trade, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT trade_id, direction, source, item_id, item_index, partial_amount, total_amount,
		       gross, setup_fee, sales_tax, net, tax_estimated, unit_silver, received, location_id
		FROM trades ORDER BY received DESC, trade_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Trade
	for rows.Next() {
		var t model.Trade
		var est int
		if err := rows.Scan(&t.TradeID, &t.Direction, &t.Source, &t.ItemID, &t.ItemIndex,
			&t.PartialAmount, &t.TotalAmount, &t.Gross, &t.SetupFee, &t.SalesTax, &t.Net,
			&est, &t.UnitSilver, &t.Received, &t.LocationID); err != nil {
			return nil, err
		}
		t.TaxEstimated = est == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

// holdingsContainersCap bounds the persisted holdings snapshot (020); mirrors the live
// aggregator's containerCap (Principle XI). Pinned (own bag/equipped) survive first.
const holdingsContainersCap = 512

// SaveContainer upserts one holdings container by its stable id (replace-on-new, FR-003),
// then prunes beyond the cap keeping pinned + newest-seen.
func (s *SQLite) SaveContainer(ctx context.Context, c holdings.ContainerSnapshot) error {
	itemsJSON, err := json.Marshal(c.Items)
	if err != nil {
		return err
	}
	pinned := 0
	if c.Pinned {
		pinned = 1
	}
	summary := 0
	if c.Summary {
		summary = 1
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO holdings_containers
		  (container_id, location, city, tab, items_json, last_seen, pinned, summary)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.GUID, string(c.Location), c.City, c.Tab, string(itemsJSON), c.LastSeen, pinned, summary,
	); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		DELETE FROM holdings_containers WHERE container_id NOT IN (
		  SELECT container_id FROM holdings_containers ORDER BY pinned DESC, last_seen DESC LIMIT ?)`,
		holdingsContainersCap)
	return err
}

// LoadContainers returns every persisted holdings container for startup hydration (020).
// A container with corrupt items_json is skipped (never crashes hydration, FR-008).
func (s *SQLite) LoadContainers(ctx context.Context) ([]holdings.ContainerSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT container_id, location, city, tab, items_json, last_seen, pinned, summary
		FROM holdings_containers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []holdings.ContainerSnapshot
	for rows.Next() {
		var c holdings.ContainerSnapshot
		var loc, itemsJSON string
		var pinned, summary int
		if err := rows.Scan(&c.GUID, &loc, &c.City, &c.Tab, &itemsJSON, &c.LastSeen, &pinned, &summary); err != nil {
			return nil, err
		}
		c.Location = model.Location(loc)
		c.Pinned = pinned == 1
		c.Summary = summary == 1
		if err := json.Unmarshal([]byte(itemsJSON), &c.Items); err != nil {
			log.Printf("store LoadContainers: skipping corrupt container %s: %v", c.GUID, err)
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteContainer removes a persisted container (e.g. an untracked-put drop).
func (s *SQLite) DeleteContainer(ctx context.Context, containerID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM holdings_containers WHERE container_id = ?`, containerID)
	return err
}

// SaveWallet upserts the single last-known wallet balance + its observation time (020 US2).
func (s *SQLite) SaveWallet(ctx context.Context, silver, lastSeen int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO wallet_state (id, silver, last_seen) VALUES (1, ?, ?)`, silver, lastSeen)
	return err
}

// LoadWallet returns the persisted wallet balance; ok=false when none was ever seen (so the
// caller keeps the honest "wallet excluded" state, FR-004).
func (s *SQLite) LoadWallet(ctx context.Context) (silver, lastSeen int64, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT silver, last_seen FROM wallet_state WHERE id=1`).Scan(&silver, &lastSeen)
	if err == sql.ErrNoRows {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	return silver, lastSeen, true, nil
}

// SaveSpecBoard upserts the single full-board snapshot (in-progress + maxed) + time (020 US3).
func (s *SQLite) SaveSpecBoard(ctx context.Context, boardJSON string, lastSeen int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO spec_board (id, board_json, last_seen) VALUES (1, ?, ?)`, boardJSON, lastSeen)
	return err
}

// LoadSpecBoard returns the persisted board snapshot; ok=false when none exists.
func (s *SQLite) LoadSpecBoard(ctx context.Context) (boardJSON string, lastSeen int64, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT board_json, last_seen FROM spec_board WHERE id=1`).Scan(&boardJSON, &lastSeen)
	if err == sql.ErrNoRows {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	return boardJSON, lastSeen, true, nil
}

// flowSessionsCap bounds the persisted completed-session history (Principle XI).
const flowSessionsCap = 200

// SaveFlowCheckpoint upserts the single live-session checkpoint (020 US4, AFM model):
// a later launch resumes from it. Older checkpoints are overwritten (one row).
func (s *SQLite) SaveFlowCheckpoint(ctx context.Context, cp flow.Checkpoint) error {
	b, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO flow_checkpoint (id, payload_json, last_activity) VALUES (1, ?, ?)`,
		string(b), cp.LastActivityMS)
	return err
}

// LoadFlowCheckpoint returns the persisted live-session checkpoint; ok=false when none or
// when the payload is corrupt (the caller then starts a clean session, FR-008).
func (s *SQLite) LoadFlowCheckpoint(ctx context.Context) (cp flow.Checkpoint, ok bool, err error) {
	var payload string
	err = s.db.QueryRowContext(ctx, `SELECT payload_json FROM flow_checkpoint WHERE id=1`).Scan(&payload)
	if err == sql.ErrNoRows {
		return flow.Checkpoint{}, false, nil
	}
	if err != nil {
		return flow.Checkpoint{}, false, err
	}
	if err := json.Unmarshal([]byte(payload), &cp); err != nil {
		log.Printf("store LoadFlowCheckpoint: corrupt payload, discarding: %v", err)
		return flow.Checkpoint{}, false, nil
	}
	return cp, true, nil
}

// DeleteFlowCheckpoint clears the live-session checkpoint (after resume or promotion).
func (s *SQLite) DeleteFlowCheckpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM flow_checkpoint`)
	return err
}

// SaveFlowSession appends one completed session to the permanent history, then prunes
// beyond the cap (newest kept).
func (s *SQLite) SaveFlowSession(ctx context.Context, cs flow.CompletedSession) error {
	items, err := json.Marshal(cs.Items)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO flow_sessions
		  (started_ms, ended_ms, active_elapsed_ms, net_silver, loot_value, gather_value, fame, silver_per_hour, items_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cs.StartedMS, cs.EndedMS, cs.ActiveElapsedMS, cs.NetSilver, cs.LootValue, cs.GatherValue, cs.Fame, cs.SilverPerHour, string(items),
	); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		DELETE FROM flow_sessions WHERE session_id NOT IN (
		  SELECT session_id FROM flow_sessions ORDER BY ended_ms DESC LIMIT ?)`, flowSessionsCap)
	return err
}

// LoadFlowSessions returns the most recent completed sessions (permanent history).
func (s *SQLite) LoadFlowSessions(ctx context.Context, limit int) ([]flow.CompletedSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT started_ms, ended_ms, active_elapsed_ms, net_silver, loot_value, gather_value, fame, silver_per_hour, items_json
		FROM flow_sessions ORDER BY ended_ms DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []flow.CompletedSession
	for rows.Next() {
		var cs flow.CompletedSession
		var items string
		if err := rows.Scan(&cs.StartedMS, &cs.EndedMS, &cs.ActiveElapsedMS, &cs.NetSilver, &cs.LootValue, &cs.GatherValue, &cs.Fame, &cs.SilverPerHour, &items); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(items), &cs.Items); err != nil {
			continue // skip a corrupt history row
		}
		out = append(out, cs)
	}
	return out, rows.Err()
}
