package indexer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eason4kim-rocket/annolux/searchindex"
)

func TestRefreshExistingDoesNotGrowOrLeaseFrontier(t *testing.T) {
	var conditional string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/robots.txt":
			_, _ = io.WriteString(writer, "User-agent: *\nCrawl-delay: 0\n")
		case "/known":
			conditional = request.Header.Get("If-None-Match")
			writer.Header().Set("ETag", `"v2"`)
			_, _ = io.WriteString(writer, `<html><title>Known</title><body>`+strings.Repeat("updated text ", 40)+`<a href="/must-not-discover">new</a></body></html>`)
		default:
			t.Fatalf("unexpected fetch of %s", request.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	store, err := searchindex.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Upsert(context.Background(), searchindex.Page{
		URL: server.URL + "/known", Root: "127.0.0.1", Title: "old", Body: "old text",
		Lang: searchindex.LangEnglish, FetchedAt: time.Now().Add(-30 * 24 * time.Hour), ETag: `"v1"`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enqueue(context.Background(), []searchindex.FrontierItem{{
		URL: server.URL + "/unrelated", Root: "unrelated.example",
	}}); err != nil {
		t.Fatal(err)
	}
	fetcher, err := NewFetcher(FetcherConfig{AllowPrivateNetworks: true})
	if err != nil {
		t.Fatal(err)
	}
	stats, err := RefreshExisting(context.Background(), store, fetcher, RefreshConfig{
		MaxPages: 10, OlderThan: time.Hour, AllowPrivate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Selected != 1 || stats.Changed != 1 || conditional != `"v1"` {
		t.Fatalf("refresh = %#v, conditional=%q", stats, conditional)
	}
	active, err := store.ActiveFrontier(context.Background())
	if err != nil || active != 1 {
		t.Fatalf("frontier after refresh = %d, %v; want unchanged", active, err)
	}
}

func TestRefreshExistingTouchesNotModifiedPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/robots.txt" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(server.Close)
	store, err := searchindex.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	old := time.Now().Add(-30 * 24 * time.Hour).UTC().Truncate(time.Second)
	if err := store.Upsert(context.Background(), searchindex.Page{
		URL: server.URL + "/known", Root: "127.0.0.1", Body: "same", Lang: searchindex.LangEnglish,
		FetchedAt: old, ETag: `"v1"`,
	}); err != nil {
		t.Fatal(err)
	}
	fetcher, err := NewFetcher(FetcherConfig{AllowPrivateNetworks: true})
	if err != nil {
		t.Fatal(err)
	}
	stats, err := RefreshExisting(context.Background(), store, fetcher, RefreshConfig{MaxPages: 1, OlderThan: time.Hour})
	if err != nil || stats.NotModified != 1 {
		t.Fatalf("refresh = %#v, %v", stats, err)
	}
	items, err := store.ListRefreshCandidates(context.Background(), time.Now().Add(-time.Hour), 10)
	if err != nil || len(items) != 0 {
		t.Fatalf("stale candidates after 304 = %#v, %v", items, err)
	}
}
