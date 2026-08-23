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

// fixture builds an index shaped like production: pages that today's junk
// rule accepts, pages it rejects for each distinct reason, one page whose
// body was never stored, and a frontier holding both kinds of queued URL.
func fixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.db")
	store, err := searchindex.Open(path)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	fetched := time.Unix(1786747096, 0).UTC()
	pages := []searchindex.Page{
		{URL: "https://developer.mozilla.org/en-US/docs/Web/API", Root: "https://developer.mozilla.org", Title: "Web API", Body: "the english reference body", Lang: "en", FetchedAt: fetched},
		{URL: "https://kubernetes.io/docs/concepts/", Root: "https://kubernetes.io", Title: "Concepts", Body: "another english body", Lang: "en", FetchedAt: fetched},
		{URL: "https://www.example.cn/zh/guide", Root: "https://www.example.cn", Title: "指南", Body: "中文正文内容", Lang: "zh", FetchedAt: fetched},
		{URL: "https://developer.mozilla.org/de/docs/Glossary/Flex", Root: "https://developer.mozilla.org", Title: "Flex", Body: "german body", Lang: "en", FetchedAt: fetched},
		{URL: "https://ab.wikipedia.org/wiki/Article", Root: "https://ab.wikipedia.org", Title: "Article", Body: "abkhaz body", Lang: "en", FetchedAt: fetched},
		{URL: "https://smalltalk.gnu.org/manual-base/manual-libs/manual-base/manual-libs/page", Root: "https://smalltalk.gnu.org", Title: "Trap", Body: "looping body", Lang: "en", FetchedAt: fetched},
		{URL: "https://example.org/paper.pdf", Root: "https://example.org", Title: "Paper", Body: "asset body", Lang: "en", FetchedAt: fetched},
		{URL: "https://example.org/zh/empty", Root: "https://example.org", Title: "Empty", Body: "placeholder", Lang: "zh", FetchedAt: fetched},
	}
	if _, err := store.UpsertMany(context.Background(), pages); err != nil {
		t.Fatalf("write pages: %v", err)
	}
	queued := []searchindex.FrontierItem{
		{URL: "https://kubernetes.io/docs/setup/", Root: "https://kubernetes.io"},
		{URL: "https://kubernetes.io/de/docs/setup/", Root: "https://kubernetes.io"},
		{URL: "https://es.tldp.org/manual", Root: "https://es.tldp.org"},
	}
	if _, err := store.Enqueue(context.Background(), queued); err != nil {
		t.Fatalf("enqueue frontier: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close index: %v", err)
	}
	// Production carries rows whose body was never stored; the fixture
	// reproduces one directly because the writer refuses to create it.
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen index: %v", err)
	}
	if _, err := database.Exec(`UPDATE pages SET body_z=NULL WHERE url='https://example.org/zh/empty'`); err != nil {
		t.Fatalf("clear body: %v", err)
	}
	if _, err := database.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close raw handle: %v", err)
	}
	if err := os.Remove(path + "-wal"); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove wal: %v", err)
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

func TestDryRunReportsWithoutDeleting(t *testing.T) {
	path := fixture(t)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat index: %v", err)
	}
	result := runTool(t, "-db", path)
	if result.Mode != "dry-run" {
		t.Fatalf("mode = %q, want dry-run", result.Mode)
	}
	if result.Before.Pages != 8 {
		t.Fatalf("pages before = %d, want 8", result.Before.Pages)
	}
	if result.JunkPages != 4 {
		t.Fatalf("junk pages = %d, want 4", result.JunkPages)
	}
	wantReasons := map[string]int64{
		"foreign_locale_path": 1,
		"foreign_locale_host": 1,
		"trapped_path":        1,
		"asset_extension":     1,
	}
	for reason, want := range wantReasons {
		if result.JunkPagesByReason[reason] != want {
			t.Fatalf("reason %s = %d, want %d", reason, result.JunkPagesByReason[reason], want)
		}
	}
	if result.EmptyBodyPages != 1 || result.EmptyBodyOnly != 1 {
		t.Fatalf("empty body = %d/%d, want 1/1", result.EmptyBodyPages, result.EmptyBodyOnly)
	}
	if result.DoomedPages != 5 || result.DoomedChinese != 1 || result.DoomedEnglish != 4 {
		t.Fatalf("doomed = %d (%d zh / %d en), want 5 (1/4)", result.DoomedPages, result.DoomedChinese, result.DoomedEnglish)
	}
	if result.JunkFrontier != 2 {
		t.Fatalf("junk frontier = %d, want 2", result.JunkFrontier)
	}
	if result.DeletedPages != 0 || result.After != nil {
		t.Fatalf("dry run reported deletions: %+v", result)
	}
	if len(result.TopHosts) == 0 || result.TopHosts[0].Pages < 1 {
		t.Fatalf("top hosts missing: %+v", result.TopHosts)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat index: %v", err)
	}
	if after.Size() != before.Size() || after.ModTime() != before.ModTime() {
		t.Fatal("dry run modified the database file")
	}
}

func TestApplyDeletesPagesFTSAndFrontier(t *testing.T) {
	path := fixture(t)
	reportPath := filepath.Join(t.TempDir(), "report.json")
	result := runTool(t, "-db", path, "-apply", "-report", reportPath)
	if result.Mode != "apply" {
		t.Fatalf("mode = %q, want apply", result.Mode)
	}
	if result.DeletedPages != 5 || result.DeletedFrontier != 2 {
		t.Fatalf("deleted %d pages / %d frontier rows, want 5/2", result.DeletedPages, result.DeletedFrontier)
	}
	if result.After == nil {
		t.Fatal("apply reported no post-state")
	}
	if result.After.Pages != 3 || result.After.EnglishPages != 2 || result.After.ChinesePages != 1 {
		t.Fatalf("surviving pages = %+v, want 3 (2 en / 1 zh)", result.After)
	}
	if result.After.EnglishFTS != 2 || result.After.ChineseFTS != 1 {
		t.Fatalf("surviving FTS rows = %d en / %d zh, want 2/1", result.After.EnglishFTS, result.After.ChineseFTS)
	}
	if result.After.Frontier != 1 {
		t.Fatalf("surviving frontier rows = %d, want 1", result.After.Frontier)
	}
	if result.Integrity != "ok" || result.FTSIntegrity != "ok" || result.SHA256 == "" {
		t.Fatalf("post-state not verified: integrity=%q fts=%q sha256=%q", result.Integrity, result.FTSIntegrity, result.SHA256)
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("report file: %v", err)
	}
	// The cleaned index must still open through the production read-only
	// path and answer a query about a page the rule kept.
	store, err := searchindex.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("reopen cleaned index: %v", err)
	}
	defer store.Close()
	results, err := store.Query(context.Background(), "english", 10)
	if err != nil {
		t.Fatalf("search cleaned index: %v", err)
	}
	for _, hit := range results {
		if hit.URL == "https://developer.mozilla.org/de/docs/Glossary/Flex" {
			t.Fatal("cleaned index still serves a rejected page")
		}
	}
	if len(results) == 0 {
		t.Fatal("cleaned index lost every English result")
	}
}

func TestWatchedURLsReportTheirFate(t *testing.T) {
	path := fixture(t)
	watchPath := filepath.Join(t.TempDir(), "watch.txt")
	watchList := "https://kubernetes.io/docs/concepts/\n" +
		"https://developer.mozilla.org/de/docs/Glossary/Flex\n" +
		"https://example.org/zh/empty\n" +
		"https://example.net/never-indexed\n"
	if err := os.WriteFile(watchPath, []byte(watchList), 0o600); err != nil {
		t.Fatalf("write watch list: %v", err)
	}
	result := runTool(t, "-db", path, "-watch-urls", watchPath)
	if len(result.WatchedURLs) != 4 || result.WatchedDoomed != 2 {
		t.Fatalf("watched = %d entries, %d doomed, want 4 and 2", len(result.WatchedURLs), result.WatchedDoomed)
	}
	byURL := map[string]watched{}
	for _, entry := range result.WatchedURLs {
		byURL[entry.URL] = entry
	}
	if kept := byURL["https://kubernetes.io/docs/concepts/"]; !kept.Indexed || kept.Doomed {
		t.Fatalf("kept target reported as %+v", kept)
	}
	if german := byURL["https://developer.mozilla.org/de/docs/Glossary/Flex"]; !german.Junk || german.Reason != "foreign_locale_path" {
		t.Fatalf("German page reported as %+v", german)
	}
	if empty := byURL["https://example.org/zh/empty"]; !empty.EmptyBody || empty.Junk {
		t.Fatalf("empty-body page reported as %+v", empty)
	}
	if missing := byURL["https://example.net/never-indexed"]; missing.Indexed || missing.Doomed {
		t.Fatalf("unindexed URL reported as %+v", missing)
	}
}

func TestReportSplitsCountsByLanguageAndClass(t *testing.T) {
	path := fixture(t)
	result := runTool(t, "-db", path)
	if result.JunkEnglish != 4 || result.JunkChinese != 0 {
		t.Fatalf("junk by language = %d en / %d zh, want 4/0", result.JunkEnglish, result.JunkChinese)
	}
	if result.EmptyBodyChinese != 1 || result.EmptyBodyEnglish != 0 {
		t.Fatalf("empty body by language = %d en / %d zh, want 0/1", result.EmptyBodyEnglish, result.EmptyBodyChinese)
	}
	for _, host := range result.TopHosts {
		if host.Pages != host.Junk+host.EmptyBody {
			continue // a page can be both junk and empty-bodied
		}
		if host.Junk == 0 && host.EmptyBody == 0 {
			t.Fatalf("host %q listed with no cause: %+v", host.Host, host)
		}
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	path := fixture(t)
	runTool(t, "-db", path, "-apply")
	second := runTool(t, "-db", path, "-apply")
	if second.DoomedPages != 0 || second.DeletedPages != 0 || second.JunkFrontier != 0 {
		t.Fatalf("second pass still found work: %+v", second)
	}
}

func TestUncheckpointedWALIsRefused(t *testing.T) {
	path := fixture(t)
	if err := os.WriteFile(path+"-wal", []byte("not empty"), 0o600); err != nil {
		t.Fatalf("write wal: %v", err)
	}
	if err := run(context.Background(), []string{"-db", path}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected refusal on an uncheckpointed WAL")
	}
}

func TestUsageErrors(t *testing.T) {
	for _, arguments := range [][]string{{}, {"-db", ""}, {"-db", "index.db", "extra"}} {
		if err := run(context.Background(), arguments, &bytes.Buffer{}); err == nil {
			t.Fatalf("expected usage error for %v", arguments)
		}
	}
}
