// Package querylog records what the hosted Search surface was asked and what
// it returned. The log exists to answer launch-time questions the account
// ledger cannot: which real queries arrive, which return nothing, and which
// raw ranking features accompanied each answer.
//
// It is telemetry, not bookkeeping: it lives in its own SQLite file, is
// written asynchronously, may drop records under pressure, and never carries
// credentials, IP addresses, or user agents. Raw query text is redacted after
// a retention window; every other column is non-personal and may be kept for
// aggregate statistics.
package querylog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DefaultRetention is how long raw query text survives before redaction.
// The privacy policy promises query content is kept only as long as request
// processing and troubleshooting need it; thirty days is that bound.
const DefaultRetention = 30 * 24 * time.Hour

const schema = `
CREATE TABLE IF NOT EXISTS query_log (
	id INTEGER PRIMARY KEY,
	request_id TEXT NOT NULL,
	ts INTEGER NOT NULL,
	source TEXT NOT NULL,
	account_id TEXT NOT NULL,
	query_text TEXT NOT NULL,
	query_redacted INTEGER NOT NULL DEFAULT 0,
	query_lang TEXT NOT NULL,
	query_has_han INTEGER NOT NULL,
	query_has_latin INTEGER NOT NULL,
	domains TEXT NOT NULL,
	http_status INTEGER NOT NULL,
	error_code TEXT NOT NULL DEFAULT '',
	result_count INTEGER NOT NULL,
	zero_result INTEGER NOT NULL,
	top_docs TEXT NOT NULL,
	candidate_count INTEGER NOT NULL,
	score_gap_top1_top2 REAL,
	signals TEXT NOT NULL,
	ranker_version TEXT NOT NULL,
	latency_ms INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS query_log_ts ON query_log(ts);
CREATE INDEX IF NOT EXISTS query_log_zero ON query_log(zero_result, ts);
CREATE INDEX IF NOT EXISTS query_log_account ON query_log(account_id, ts);
`

// TopDoc is one returned document: enough to join back to the index and to
// recompute any future confidence formula, small enough to store per request.
type TopDoc struct {
	URLHash string  `json:"h"`
	Rank    int     `json:"r"`
	Score   float64 `json:"s"`
	Lang    string  `json:"l"`
}

// Signals carries the raw boundary observations that a lexical-confidence
// formula may later be built from. Individual fields, never a precomputed
// confidence: the formula will change, the features must not.
type Signals struct {
	DomainFiltered int   `json:"domain_filtered"`
	NearDupRemoved int   `json:"near_dup_removed"`
	OtherLangHits  int   `json:"other_lang_hits"`
	Deduplicated   int   `json:"deduplicated"`
	Partial        bool  `json:"partial"`
	Fused          bool  `json:"fused"`
	Tier1Hits      int   `json:"tier1_hits"`
	ProviderMs     int64 `json:"provider_ms"`
	TotalMs        int64 `json:"total_ms"`
}

// Record is one observed Search request. Absent observation fields stay zero;
// ScoreGap is nil when fewer than two candidates existed.
type Record struct {
	RequestID     string
	Time          time.Time
	Source        string
	AccountID     string
	QueryText     string
	QueryLang     string
	Domains       []string
	HTTPStatus    int
	ErrorCode     string
	ResultCount   int
	TopDocs       []TopDoc
	Candidates    int
	ScoreGap      *float64
	Signals       Signals
	RankerVersion string
	LatencyMS     int64
}

// Stats summarizes the log for operations checks.
type Stats struct {
	Total       int64 `json:"total"`
	ZeroResults int64 `json:"zero_results"`
	Redacted    int64 `json:"redacted"`
	OldestTS    int64 `json:"oldest_ts"`
	NewestTS    int64 `json:"newest_ts"`
}

// Store owns the query-log SQLite file. All writes go through one connection;
// the log's durability posture is deliberately weaker than the ledgers'.
type Store struct {
	db   *sql.DB
	path string
}

// Open creates or opens the log database. The file is chmodded to 0600: query
// text is user content even though nothing else in the row is personal.
func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("querylog: database path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("querylog: resolve path: %w", err)
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute, RawQuery: url.Values{
		"_busy_timeout": []string{"5000"},
		"_journal_mode": []string{"wal"},
		"_synchronous":  []string{"normal"},
		"_txlock":       []string{"immediate"},
	}.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("querylog: open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("querylog: apply schema: %w", err)
	}
	// The schema write above has already forced the WAL sidecars into
	// existence, and the driver creates them from the umask rather than from
	// the database file's mode — query text passes through the WAL, so the
	// sidecars need the same 0600 as the main file.
	for _, path := range []string{absolute, absolute + "-wal", absolute + "-shm"} {
		if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrPermission) && !errors.Is(err, os.ErrNotExist) {
			_ = db.Close()
			return nil, fmt.Errorf("querylog: secure database: %w", err)
		}
	}
	return &Store{db: db, path: absolute}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// InsertBatch writes records in one transaction. A failed batch is dropped as
// a whole; the caller only counts it, because the log must never propagate
// its failures into the request path.
func (s *Store) InsertBatch(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO query_log(
			request_id, ts, source, account_id,
			query_text, query_redacted, query_lang, query_has_han, query_has_latin,
			domains, http_status, error_code, result_count, zero_result,
			top_docs, candidate_count, score_gap_top1_top2, signals,
			ranker_version, latency_ms
		) VALUES (?,?,?,?,?,0,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, record := range records {
		domains, err := json.Marshal(emptyIfNil(record.Domains))
		if err != nil {
			return err
		}
		topDocs, err := json.Marshal(emptyDocsIfNil(record.TopDocs))
		if err != nil {
			return err
		}
		signals, err := json.Marshal(record.Signals)
		if err != nil {
			return err
		}
		hasHan, hasLatin := scriptFlags(record.QueryText)
		zero := 0
		if record.HTTPStatus == 200 && record.ResultCount == 0 {
			zero = 1
		}
		var gap any
		if record.ScoreGap != nil {
			gap = *record.ScoreGap
		}
		if _, err := statement.ExecContext(ctx,
			record.RequestID, record.Time.UTC().Unix(), record.Source, record.AccountID,
			record.QueryText, record.QueryLang, boolToInt(hasHan), boolToInt(hasLatin),
			string(domains), record.HTTPStatus, record.ErrorCode, record.ResultCount, zero,
			string(topDocs), record.Candidates, gap, string(signals),
			record.RankerVersion, record.LatencyMS,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ExpireQueryText redacts raw query text older than the cutoff. Rows survive
// with their non-personal columns so long-term aggregates stay computable.
func (s *Store) ExpireQueryText(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE query_log SET query_text = '', query_redacted = 1
		 WHERE ts < ? AND query_redacted = 0`, cutoff.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("querylog: expire: %w", err)
	}
	return result.RowsAffected()
}

// Stats reports aggregate counters for operational verification.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(zero_result), 0),
		       COALESCE(SUM(query_redacted), 0),
		       COALESCE(MIN(ts), 0),
		       COALESCE(MAX(ts), 0)
		FROM query_log`).Scan(&stats.Total, &stats.ZeroResults, &stats.Redacted, &stats.OldestTS, &stats.NewestTS)
	if err != nil {
		return Stats{}, fmt.Errorf("querylog: stats: %w", err)
	}
	return stats, nil
}

// HashURL is the stable document identifier stored in top_docs: enough bits
// to join against a hashed index URL list, no reversible URL in the log.
func HashURL(rawURL string) string {
	digest := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(digest[:8])
}

// scriptFlags records which scripts the query used. The flags survive text
// redaction, so language-mix statistics stay computable after thirty days.
func scriptFlags(text string) (hasHan, hasLatin bool) {
	for _, r := range text {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF || r >= 0x3400 && r <= 0x4DBF:
			hasHan = true
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			hasLatin = true
		}
		if hasHan && hasLatin {
			return
		}
	}
	return
}

func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func emptyDocsIfNil(docs []TopDoc) []TopDoc {
	if docs == nil {
		return []TopDoc{}
	}
	return docs
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
