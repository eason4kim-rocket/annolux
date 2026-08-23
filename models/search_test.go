package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSearchDefaultsAreCommercialDefaults(t *testing.T) {
	request := SearchRequest{}
	request.Defaults()
	if request.Limit != 10 || request.Timeout != 30 || request.Deduplicate == nil || !*request.Deduplicate {
		t.Fatalf("Defaults() = %#v", request)
	}
}

func TestSearchRequestHasOnlyCommercialFields(t *testing.T) {
	encoded, err := json.Marshal(SearchRequest{
		Query: "rust ownership", Domains: []string{"doc.rust-lang.org"},
		Limit: 5, Timeout: 30, Ranking: SearchRankingProvider, Freshness: "day",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"include_content", "verify", "schema", "llm_api_key", "expected_subject", "trust"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("commercial request leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestSearchResultPublishesFetchTime(t *testing.T) {
	fetchedAt := time.Date(2026, 8, 16, 1, 2, 3, 0, time.UTC)
	encoded, err := json.Marshal(SearchResult{
		Rank: 1, Title: "Rust", URL: "https://doc.rust-lang.org/", FetchedAt: &fetchedAt,
	})
	if err != nil || !strings.Contains(string(encoded), `"fetched_at":"2026-08-16T01:02:03Z"`) {
		t.Fatalf("Marshal() = %s, %v", encoded, err)
	}
}
