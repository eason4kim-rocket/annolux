package searchindex

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRefreshCandidatesAndMetadataOnlyUpdates(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	old := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	page := Page{URL: "https://example.com/old", Root: "example.com", Title: "old", Body: "body", Lang: LangEnglish, FetchedAt: old, ETag: `"v1"`}
	if err := store.Upsert(context.Background(), page); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListRefreshCandidates(context.Background(), time.Now(), 10)
	if err != nil || len(items) != 1 || items[0].ETag != `"v1"` {
		t.Fatalf("ListRefreshCandidates() = %#v, %v", items, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.TouchPage(context.Background(), page.URL, now, `"v2"`, "Mon, 01 Jan 2024 00:00:00 GMT"); err != nil {
		t.Fatal(err)
	}
	items, err = store.ListRefreshCandidates(context.Background(), now.Add(time.Second), 10)
	if err != nil || len(items) != 1 || !items[0].FetchedAt.Equal(now) || items[0].ETag != `"v2"` {
		t.Fatalf("metadata after touch = %#v, %v", items, err)
	}

	// A same-body 200 advances validators/fetch time without rewriting FTS.
	page.FetchedAt = now.Add(time.Minute)
	page.ETag = `"v3"`
	written, err := store.UpsertMany(context.Background(), []Page{page})
	if err != nil || written != 0 {
		t.Fatalf("same-content UpsertMany() = %d, %v", written, err)
	}
	items, err = store.ListRefreshCandidates(context.Background(), now.Add(2*time.Minute), 10)
	if err != nil || len(items) != 1 || items[0].ETag != `"v3"` || !items[0].FetchedAt.Equal(page.FetchedAt) {
		t.Fatalf("metadata after same-content upsert = %#v, %v", items, err)
	}
}
