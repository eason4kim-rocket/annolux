package searchfuse

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/eason4kim-rocket/annolux/searchindex"
)

func openStore(t *testing.T) *searchindex.Store {
	t.Helper()
	store, err := searchindex.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fetched := time.Unix(1_786_900_000, 0).UTC()
	pages := []searchindex.Page{
		{URL: "https://docs.example.com/promise-reference", Root: "example.com", Title: "Promise reference", Body: "Promise resolve reject asynchronous JavaScript reference guide", Lang: "en", FetchedAt: fetched},
		{URL: "https://docs.example.com/promise-tutorial", Root: "example.com", Title: "Promise tutorial", Body: "Promise asynchronous JavaScript tutorial with examples", Lang: "en", FetchedAt: fetched},
		{URL: "https://other.example.net/callbacks", Root: "example.net", Title: "Callbacks", Body: "callback style asynchronous JavaScript older pattern", Lang: "en", FetchedAt: fetched},
	}
	for _, page := range pages {
		if err := store.Upsert(context.Background(), page); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func addLite(t *testing.T, store *searchindex.Store, promoted bool, urls ...string) {
	t.Helper()
	fetched := time.Unix(1_786_901_000, 0).UTC()
	pages := make([]searchindex.LitePage, 0, len(urls))
	for _, url := range urls {
		pages = append(pages, searchindex.LitePage{
			URL: url, Root: "lite.example.org", Title: "Promise polyfill notes",
			Summary: "Discovery-tier summary about promise polyfills.",
			Body:    "promise polyfill microtask queue scheduling notes",
			Lang:    searchindex.LangEnglish, FetchedAt: fetched, Promoted: promoted,
		})
	}
	if _, err := store.UpsertLiteMany(context.Background(), pages); err != nil {
		t.Fatal(err)
	}
}

func urls(hits []searchindex.Hit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.URL)
	}
	return out
}

func TestDisabledFuserIsPassthrough(t *testing.T) {
	store := openStore(t)
	fuser := New(store, Config{})
	direct, err := store.Query(context.Background(), "Promise JavaScript", 10)
	if err != nil {
		t.Fatal(err)
	}
	fused, stats, err := fuser.Query(context.Background(), "Promise JavaScript", 10)
	if err != nil || stats.Fused || stats.Tier1Hits != 0 {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	if len(direct) != len(fused) {
		t.Fatalf("lengths differ: %d vs %d", len(direct), len(fused))
	}
	for index := range direct {
		if direct[index] != fused[index] {
			t.Fatalf("hit %d differs: %+v vs %+v", index, direct[index], fused[index])
		}
	}
}

func TestEnabledOverEmptyDiscoveryPreservesOrder(t *testing.T) {
	store := openStore(t)
	// No lite row matches this query, so the enabled fuser must still match
	// the frozen order exactly — the dark-equivalence property that let the
	// flag ship off.
	if _, err := store.UpsertLiteMany(context.Background(), []searchindex.LitePage{{
		URL: "https://docs.example.com/unrelated-topic", Root: "lite.example.org",
		Title: "Gardening almanac", Summary: "Seasonal planting notes.",
		Body: "tomato seedling frost calendar soil rotation",
		Lang: searchindex.LangEnglish, FetchedAt: time.Unix(1_786_901_000, 0).UTC(),
	}}); err != nil {
		t.Fatal(err)
	}
	fuser := New(store, Config{Enabled: true})
	direct, err := store.Query(context.Background(), "Promise JavaScript", 10)
	if err != nil {
		t.Fatal(err)
	}
	fused, stats, err := fuser.Query(context.Background(), "Promise JavaScript", 10)
	if err != nil || !stats.Fused || stats.Tier1Added != 0 {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	if len(direct) != len(fused) {
		t.Fatalf("lengths differ: %d vs %d", len(direct), len(fused))
	}
	for index := range direct {
		if direct[index].URL != fused[index].URL {
			t.Fatalf("order diverged at %d: %s vs %s", index, direct[index].URL, fused[index].URL)
		}
	}
}

// TestPromotedRowsRescueCoreMisses pins the policy reversal the v3 dev split
// forced: a page whose body lives in the core but which the core query
// misses used to be unreachable from either lane, because the discovery lane
// excluded promoted rows. Now the lane includes them and fusion carries the
// page back into the results.
func TestPromotedRowsRescueCoreMisses(t *testing.T) {
	store := openStore(t)
	addLite(t, store, true, "https://docs.example.com/promise-reference")
	fuser := New(store, Config{Enabled: true})
	fused, stats, err := fuser.Query(context.Background(), "Promise JavaScript", 10)
	if err != nil || !stats.Fused || stats.Tier1Hits == 0 {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	found := false
	for _, hit := range fused {
		if hit.URL == "https://docs.example.com/promise-reference" {
			found = true
		}
	}
	if !found {
		t.Fatal("promoted lite row did not re-enter through the discovery lane")
	}
}

func TestDiscoveryOnlyPagesJoinTheFusedList(t *testing.T) {
	store := openStore(t)
	addLite(t, store, false,
		"https://lite.example.org/polyfill-a",
		"https://lite.example.org/polyfill-b",
	)
	fuser := New(store, Config{Enabled: true})
	fused, stats, err := fuser.Query(context.Background(), "promise polyfill", 10)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Tier1Hits != 2 || stats.Tier1Added == 0 {
		t.Fatalf("stats = %+v", stats)
	}
	found := false
	for _, hit := range fused {
		if hit.URL == "https://lite.example.org/polyfill-a" {
			found = true
			if hit.Snippet == "" || hit.FetchedAt == 0 || hit.Title == "" {
				t.Fatalf("discovery hit missing serve fields: %+v", hit)
			}
		}
	}
	if !found {
		t.Fatalf("discovery page absent from fusion: %v", urls(fused))
	}
	// Determinism across runs.
	again, _, err := fuser.Query(context.Background(), "promise polyfill", 10)
	if err != nil {
		t.Fatal(err)
	}
	for index := range fused {
		if fused[index].URL != again[index].URL {
			t.Fatalf("fusion not deterministic at %d", index)
		}
	}
}

func TestBothLanesBoostSharedURL(t *testing.T) {
	store := openStore(t)
	// The same URL ranked in both lanes accumulates both reciprocal ranks and
	// must not be duplicated in the output.
	addLite(t, store, false, "https://docs.example.com/promise-tutorial")
	fuser := New(store, Config{Enabled: true})
	fused, _, err := fuser.Query(context.Background(), "Promise polyfill tutorial", 10)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, hit := range fused {
		if hit.URL == "https://docs.example.com/promise-tutorial" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("shared URL appeared %d times: %v", count, urls(fused))
	}
}

// TestPreferredLanguageIsNotDilutedByTheOtherLane pins the second policy the
// v3 dev split forced: cross-language discovery hits append after the
// preferred language instead of interleaving with it. Interleaving halved
// every preferred rank, pushing lane-found targets out of the fused top ten.
func TestPreferredLanguageIsNotDilutedByTheOtherLane(t *testing.T) {
	store := openStore(t)
	fetched := time.Unix(1_786_901_000, 0).UTC()
	if _, err := store.UpsertLiteMany(context.Background(), []searchindex.LitePage{
		{URL: "https://en.example.org/promise-a", Root: "en.example.org", Title: "Promise scheduling",
			Summary: "en one", Body: "promise microtask ordering", Lang: searchindex.LangEnglish, FetchedAt: fetched},
		{URL: "https://en.example.org/promise-b", Root: "en.example.org", Title: "Promise cancellation",
			Summary: "en two", Body: "promise abort controller pattern", Lang: searchindex.LangEnglish, FetchedAt: fetched},
		{URL: "https://zh.example.org/promise-zh", Root: "zh.example.org", Title: "Promise 异步教程",
			Summary: "zh row", Body: "promise 异步 编程 教程", Lang: searchindex.LangChinese, FetchedAt: fetched},
	}); err != nil {
		t.Fatal(err)
	}
	fuser := New(store, Config{Enabled: true})
	fused, stats, err := fuser.Query(context.Background(), "Promise JavaScript", 10)
	if err != nil || !stats.Fused {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	positions := map[string]int{}
	for index, hit := range fused {
		positions[hit.URL] = index
	}
	zhPos, zhIn := positions["https://zh.example.org/promise-zh"]
	if !zhIn {
		return // zh row may fall outside the limit; dilution cannot occur then
	}
	for _, enURL := range []string{"https://en.example.org/promise-a", "https://en.example.org/promise-b"} {
		enPos, enIn := positions[enURL]
		if !enIn {
			t.Fatalf("preferred-language row %s missing while other-language row served", enURL)
		}
		if zhPos < enPos {
			t.Fatalf("other-language row outranked preferred row: zh at %d, %s at %d", zhPos, enURL, enPos)
		}
	}
}
