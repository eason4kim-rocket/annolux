package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eason4kim-rocket/annolux/searchindex"
)

// fixture builds an index with every migration shape: an https/http pair, a
// short/long path pair, a three-way group holding one protected evaluation
// target, a group holding two targets (must be skipped whole), and unique
// pages that must survive untouched.
func fixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.db")
	store, err := searchindex.Open(path)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	fetched := time.Unix(1_786_900_000, 0).UTC()
	pages := []searchindex.Page{
		// Group 1: scheme decides; https wins.
		{URL: "http://docs.python.org/3/tutorial/venv.html", Root: "docs.python.org", Title: "venv", Body: "virtual environment tutorial body", Lang: "en"},
		{URL: "https://docs.python.org/3/tutorial/venv.html", Root: "docs.python.org", Title: "venv", Body: "virtual environment tutorial body", Lang: "en"},
		// Group 2: path depth decides; the shallow URL wins.
		{URL: "https://example.com/docs/deep/nested/page", Root: "example.com", Title: "Page", Body: "the very same body text twice", Lang: "en"},
		{URL: "https://example.com/page", Root: "example.com", Title: "Page", Body: "the very same body text twice", Lang: "en"},
		// Group 3: three ways, one is a protected target that would lose on
		// scoring (longer path) but must win anyway.
		{URL: "https://kubernetes.io/docs/concepts/ingress/", Root: "kubernetes.io", Title: "Ingress", Body: "ingress duplicate body", Lang: "en"},
		{URL: "https://kubernetes.io/ingress", Root: "kubernetes.io", Title: "Ingress", Body: "ingress duplicate body", Lang: "en"},
		{URL: "https://kubernetes.io/docs/concepts/services-networking/ingress/", Root: "kubernetes.io", Title: "Ingress", Body: "ingress duplicate body", Lang: "en"},
		// Group 4: two protected targets share one body; skipped whole.
		{URL: "https://example.org/target-a", Root: "example.org", Title: "T", Body: "shared target body", Lang: "en"},
		{URL: "https://example.org/target-b", Root: "example.org", Title: "T", Body: "shared target body", Lang: "en"},
		// Chinese duplicate pair.
		{URL: "https://example.cn/zh/guide?ref=home", Root: "example.cn", Title: "指南", Body: "中文重复正文内容", Lang: "zh"},
		{URL: "https://example.cn/zh/guide", Root: "example.cn", Title: "指南", Body: "中文重复正文内容", Lang: "zh"},
		// Unique pages.
		{URL: "https://unique.example.net/a", Root: "example.net", Title: "A", Body: "completely unique body a", Lang: "en"},
		{URL: "https://unique.example.cn/b", Root: "example.cn", Title: "乙", Body: "完全独立的中文正文", Lang: "zh"},
	}
	for index, page := range pages {
		page.FetchedAt = fetched.Add(time.Duration(index) * time.Second)
		if err := store.Upsert(context.Background(), page); err != nil {
			t.Fatalf("write %s: %v", page.URL, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close index: %v", err)
	}
	if err := os.Remove(path + "-wal"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return path
}

func writeWatch(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "watch.txt")
	watch := "https://kubernetes.io/docs/concepts/services-networking/ingress/\n" +
		"https://example.org/target-a\n" +
		"https://example.org/target-b\n"
	if err := os.WriteFile(path, []byte(watch), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runTool(t *testing.T, arguments ...string) report {
	t.Helper()
	var out bytes.Buffer
	if err := run(context.Background(), arguments, &out); err != nil {
		t.Fatalf("run %v: %v", arguments, err)
	}
	var decoded report
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	return decoded
}

func TestDryRunMeasuresWithoutWriting(t *testing.T) {
	path := fixture(t)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	result := runTool(t, "-db", path, "-watch-urls", writeWatch(t))
	if result.Mode != "dry-run" {
		t.Fatalf("mode = %q", result.Mode)
	}
	if result.Bef.Pages != 13 || result.DistinctHashes != 7 {
		t.Fatalf("pages=%d distinct=%d, want 13/7", result.Bef.Pages, result.DistinctHashes)
	}
	// Four collapsible groups (one skipped): losers = 1+1+2+1 = 5.
	if result.DuplicateGroups != 4 || result.DuplicateURLs != 5 {
		t.Fatalf("groups=%d losers=%d, want 4/5", result.DuplicateGroups, result.DuplicateURLs)
	}
	if result.DuplicateEN != 4 || result.DuplicateZH != 1 {
		t.Fatalf("dup en=%d zh=%d, want 4/1", result.DuplicateEN, result.DuplicateZH)
	}
	if result.SkippedGroups != 1 || result.WatchedProtected != 3 {
		t.Fatalf("skipped=%d protected=%d, want 1/3", result.SkippedGroups, result.WatchedProtected)
	}
	if result.DistinctEN+result.DistinctZH != result.DistinctHashes {
		t.Fatalf("distinct split %d+%d != %d", result.DistinctEN, result.DistinctZH, result.DistinctHashes)
	}
	if result.URLBytesTotal <= 0 || result.LongestURL <= 0 || result.AliasedURLs != 0 || result.After != nil {
		t.Fatalf("unexpected report: %+v", result)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() || after.ModTime() != before.ModTime() {
		t.Fatal("dry run modified the database")
	}
}

func TestApplyDemotesLosersToAliases(t *testing.T) {
	path := fixture(t)
	result := runTool(t, "-db", path, "-apply", "-watch-urls", writeWatch(t))
	if result.AliasedURLs != 5 || result.After == nil {
		t.Fatalf("aliased=%d after=%v", result.AliasedURLs, result.After)
	}
	if result.After.Pages != 8 || result.After.Aliases != 5 {
		t.Fatalf("after = %+v", result.After)
	}
	if result.After.EnglishPages != result.After.EnglishFTS || result.After.ChinesePages != result.After.ChineseFTS {
		t.Fatalf("FTS mismatch: %+v", result.After)
	}
	if result.Integrity != "ok" || result.FTSIntegrity != "ok" || result.SHA256 == "" {
		t.Fatalf("verification missing: %+v", result)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The https URL won group 1; the http twin is an alias pointing at it.
	var canonicalID int64
	if err := db.QueryRow(`SELECT id FROM pages WHERE url='https://docs.python.org/3/tutorial/venv.html'`).Scan(&canonicalID); err != nil {
		t.Fatalf("canonical vanished: %v", err)
	}
	var aliasTarget int64
	var aliasFetched int64
	if err := db.QueryRow(`SELECT page_id, fetched_at FROM url_aliases WHERE url='http://docs.python.org/3/tutorial/venv.html'`).Scan(&aliasTarget, &aliasFetched); err != nil {
		t.Fatalf("alias missing: %v", err)
	}
	if aliasTarget != canonicalID || aliasFetched == 0 {
		t.Fatalf("alias row wrong: target=%d fetched=%d", aliasTarget, aliasFetched)
	}
	// The protected target beat two better-scoring URLs.
	var protected int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pages WHERE url='https://kubernetes.io/docs/concepts/services-networking/ingress/'`).Scan(&protected); err != nil || protected != 1 {
		t.Fatalf("protected target: %d, %v", protected, err)
	}
	// The two-target group survived whole.
	var shared int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pages WHERE url LIKE 'https://example.org/target-%'`).Scan(&shared); err != nil || shared != 2 {
		t.Fatalf("skipped group: %d, %v", shared, err)
	}
	// The query-string zh URL lost to the clean one.
	var zhCanonical int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pages WHERE url='https://example.cn/zh/guide'`).Scan(&zhCanonical); err != nil || zhCanonical != 1 {
		t.Fatalf("zh canonical: %d, %v", zhCanonical, err)
	}
	// The migrated database opens through the production read-only path of a
	// binary carrying migration V3 and still answers queries.
	store, err := searchindex.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("reopen migrated index: %v", err)
	}
	defer store.Close()
	hits, err := store.Query(context.Background(), "ingress", 10)
	if err != nil || len(hits) == 0 {
		t.Fatalf("query after migration: %d hits, %v", len(hits), err)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	path := fixture(t)
	watch := writeWatch(t)
	runTool(t, "-db", path, "-apply", "-watch-urls", watch)
	second := runTool(t, "-db", path, "-apply", "-watch-urls", watch)
	if second.DuplicateURLs != 0 || second.AliasedURLs != 0 {
		t.Fatalf("second pass still found work: %+v", second)
	}
	if second.SkippedGroups != 1 {
		t.Fatalf("skipped group not stable: %+v", second)
	}
}

func TestUsageAndWALGuards(t *testing.T) {
	for _, arguments := range [][]string{{}, {"-db", ""}, {"-db", "x", "extra"}} {
		if err := run(context.Background(), arguments, &bytes.Buffer{}); err == nil {
			t.Fatalf("expected usage error for %v", arguments)
		}
	}
	path := fixture(t)
	if err := os.WriteFile(path+"-wal", []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"-db", path}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected refusal on an uncheckpointed WAL")
	}
}
