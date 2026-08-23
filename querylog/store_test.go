package querylog

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "querylog.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func sampleRecord(query string, when time.Time) Record {
	gap := 1.5
	return Record{
		RequestID: "req-1", Time: when, Source: "api", AccountID: "acct-1",
		QueryText: query, QueryLang: "en", Domains: []string{"kubernetes.io"},
		HTTPStatus: 200, ResultCount: 2,
		TopDocs: []TopDoc{
			{URLHash: HashURL("https://kubernetes.io/docs/a"), Rank: 1, Score: -4.2, Lang: "en"},
			{URLHash: HashURL("https://kubernetes.io/docs/b"), Rank: 2, Score: -3.9, Lang: "en"},
		},
		Candidates: 20, ScoreGap: &gap,
		Signals:       Signals{DomainFiltered: 3, NearDupRemoved: 1, ProviderMs: 40, TotalMs: 45},
		RankerVersion: "test-commit", LatencyMS: 52,
	}
}

func TestInsertAndReadBack(t *testing.T) {
	store, path := openTestStore(t)
	now := time.Date(2026, 8, 17, 6, 0, 0, 0, time.UTC)
	records := []Record{
		sampleRecord("kubernetes ingress", now),
		{RequestID: "req-2", Time: now, Source: "playground", AccountID: "acct-2",
			QueryText: "没有结果的查询", QueryLang: "zh", HTTPStatus: 200, ResultCount: 0,
			RankerVersion: "test-commit", LatencyMS: 8},
		{RequestID: "req-3", Time: now, Source: "api", AccountID: "acct-1",
			QueryText: "slow query", QueryLang: "en", HTTPStatus: 504, ErrorCode: "SEARCH_TIMEOUT",
			RankerVersion: "test-commit", LatencyMS: 5000},
	}
	if err := store.InsertBatch(context.Background(), records); err != nil {
		t.Fatalf("insert: %v", err)
	}
	stats, err := store.Stats(context.Background())
	if err != nil || stats.Total != 3 {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	// Only the empty 200 counts as a zero result; the timeout does not.
	if stats.ZeroResults != 1 || stats.Redacted != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var lang string
	var hasHan, hasLatin, zero int
	var topDocs, domains string
	var gap sql.NullFloat64
	err = db.QueryRow(`SELECT query_lang, query_has_han, query_has_latin, zero_result, top_docs, domains, score_gap_top1_top2
		FROM query_log WHERE request_id='req-1'`).Scan(&lang, &hasHan, &hasLatin, &zero, &topDocs, &domains, &gap)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if lang != "en" || hasHan != 0 || hasLatin != 1 || zero != 0 || !gap.Valid || gap.Float64 != 1.5 {
		t.Fatalf("row = lang=%s han=%d latin=%d zero=%d gap=%+v", lang, hasHan, hasLatin, zero, gap)
	}
	var decoded []TopDoc
	if err := json.Unmarshal([]byte(topDocs), &decoded); err != nil || len(decoded) != 2 || decoded[0].Rank != 1 {
		t.Fatalf("top_docs = %s, %v", topDocs, err)
	}
	if domains != `["kubernetes.io"]` {
		t.Fatalf("domains = %s", domains)
	}
	var hanFlag, timeoutGap int
	if err := db.QueryRow(`SELECT query_has_han, score_gap_top1_top2 IS NULL FROM query_log WHERE request_id='req-2'`).Scan(&hanFlag, &timeoutGap); err != nil || hanFlag != 1 || timeoutGap != 1 {
		t.Fatalf("zh row: han=%d gapnull=%d err=%v", hanFlag, timeoutGap, err)
	}
}

func TestFilePermissionsAre0600(t *testing.T) {
	store, path := openTestStore(t)
	// A write forces the WAL sidecars into existence; query text passes
	// through them, so they must be as private as the main file.
	if err := store.InsertBatch(context.Background(), []Record{sampleRecord("perm probe", time.Now())}); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(candidate)
		if os.IsNotExist(err) && candidate != path {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Fatalf("%s mode = %o, want 0600", candidate, mode)
		}
	}
}

func TestExpireRedactsOnlyOldQueryText(t *testing.T) {
	store, _ := openTestStore(t)
	now := time.Now().UTC()
	old := sampleRecord("thirty one days old", now.Add(-31*24*time.Hour))
	old.RequestID = "req-old"
	fresh := sampleRecord("fresh query", now)
	fresh.RequestID = "req-fresh"
	if err := store.InsertBatch(context.Background(), []Record{old, fresh}); err != nil {
		t.Fatal(err)
	}
	redacted, err := store.ExpireQueryText(context.Background(), now.Add(-DefaultRetention))
	if err != nil || redacted != 1 {
		t.Fatalf("expire = %d, %v", redacted, err)
	}
	stats, err := store.Stats(context.Background())
	if err != nil || stats.Redacted != 1 || stats.Total != 2 {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	// A second pass finds nothing: redaction is idempotent.
	redacted, err = store.ExpireQueryText(context.Background(), now.Add(-DefaultRetention))
	if err != nil || redacted != 0 {
		t.Fatalf("second expire = %d, %v", redacted, err)
	}
	db, err := sql.Open("sqlite", store.path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var oldText, freshText string
	if err := db.QueryRow(`SELECT query_text FROM query_log WHERE request_id='req-old'`).Scan(&oldText); err != nil || oldText != "" {
		t.Fatalf("old text = %q, %v", oldText, err)
	}
	if err := db.QueryRow(`SELECT query_text FROM query_log WHERE request_id='req-fresh'`).Scan(&freshText); err != nil || freshText != "fresh query" {
		t.Fatalf("fresh text = %q, %v", freshText, err)
	}
}

func TestLoggerWritesAsynchronously(t *testing.T) {
	store, _ := openTestStore(t)
	logger := NewLogger(store, nil, DefaultRetention)
	for index := 0; index < 10; index++ {
		logger.Log(sampleRecord("async", time.Now()))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := logger.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	stats, err := store.Stats(context.Background())
	if err != nil || stats.Total != 10 {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if logger.Dropped() != 0 {
		t.Fatalf("dropped = %d", logger.Dropped())
	}
}

func TestLoggerNeverBlocksWhenQueueOverflows(t *testing.T) {
	store, _ := openTestStore(t)
	logger := NewLogger(store, nil, DefaultRetention)
	defer logger.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for index := 0; index < queueCapacity*4; index++ {
			logger.Log(sampleRecord("burst", time.Now()))
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Log blocked under burst load")
	}
}

func TestLoggerSurvivesWriteFailure(t *testing.T) {
	store, path := openTestStore(t)
	logger := NewLogger(store, nil, DefaultRetention)
	// Closing the underlying handle makes every write fail, standing in for
	// a read-only disk. Log must stay non-blocking and Close must not hang.
	_ = store.db.Close()
	for index := 0; index < 5; index++ {
		logger.Log(sampleRecord("failing", time.Now()))
	}
	closed := make(chan error, 1)
	go func() { closed <- logger.Close() }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close hung on failing store")
	}
	if logger.Dropped() == 0 {
		t.Fatal("write failures were not counted as drops")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestHashURLIsStableAndOpaque(t *testing.T) {
	first := HashURL("https://kubernetes.io/docs/a")
	second := HashURL("https://kubernetes.io/docs/a")
	other := HashURL("https://kubernetes.io/docs/b")
	if first != second || first == other || len(first) != 16 {
		t.Fatalf("hashes: %q %q %q", first, second, other)
	}
}
