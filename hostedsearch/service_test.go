package hostedsearch

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eason4kim-rocket/annolux/models"
	"github.com/eason4kim-rocket/annolux/searchindex"
)

func TestServiceSearchKeepsPageSizeStableAndPublishesFetchTime(t *testing.T) {
	store := openStore(t)
	for index, page := range []searchindex.Page{
		{URL: "https://docs.example.com/a", Root: "example.com", Title: "Promise reference", Body: "Promise resolve reject asynchronous JavaScript reference", Lang: searchindex.LangEnglish},
		{URL: "https://docs.example.com/b", Root: "example.com", Title: "Promise tutorial", Body: "Promise asynchronous JavaScript tutorial examples", Lang: searchindex.LangEnglish},
		{URL: "https://elsewhere.example.net/c", Root: "example.net", Title: "Promise guide", Body: "Promise JavaScript guide", Lang: searchindex.LangEnglish},
	} {
		page.FetchedAt = time.Unix(1_700_000_000+int64(index), 0).UTC()
		if err := store.Upsert(context.Background(), page); err != nil {
			t.Fatal(err)
		}
	}
	service, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	five, err := service.Search(context.Background(), &models.SearchRequest{Query: "Promise JavaScript", Limit: 5})
	if err != nil || len(five.Results) < 2 {
		t.Fatalf("Search(5) = %#v, %v", five, err)
	}
	ten, err := service.Search(context.Background(), &models.SearchRequest{Query: "Promise JavaScript", Limit: 10})
	if err != nil || len(ten.Results) < len(five.Results) {
		t.Fatalf("Search(10) = %#v, %v", ten, err)
	}
	for index := range five.Results {
		if five.Results[index].URL != ten.Results[index].URL {
			t.Fatalf("page-size drift at %d: %s != %s", index, five.Results[index].URL, ten.Results[index].URL)
		}
		if five.Results[index].FetchedAt == nil || five.Results[index].FetchedAt.IsZero() {
			t.Fatalf("missing fetched_at: %#v", five.Results[index])
		}
	}
	filtered, err := service.Search(context.Background(), &models.SearchRequest{Query: "Promise JavaScript", Domains: []string{"example.com"}})
	if err != nil || len(filtered.Results) != 2 {
		t.Fatalf("domain Search = %#v, %v", filtered, err)
	}
}

func TestServiceRejectsEveryNonCommercialOption(t *testing.T) {
	store := openStore(t)
	service, _ := New(store)
	tests := []*models.SearchRequest{
		{Query: "q", Freshness: "day"},
		{Query: "q", Ranking: models.SearchRankingMode("trust")},
		{Query: "q", Limit: 11},
		{Query: "q", Timeout: 31},
		{Query: "q", Domains: []string{"com"}},
		{Query: "q", Domains: []string{"https://example.com"}},
	}
	for _, request := range tests {
		_, err := service.Search(context.Background(), request)
		var typed *models.ServiceError
		if !errors.As(err, &typed) || typed.Code != models.ErrCodeInvalidInput {
			t.Fatalf("Search(%#v) = %v", request, err)
		}
	}
}

func TestServiceDefaultsDeduplicationAndDoesNotMutateRequest(t *testing.T) {
	store := openStore(t)
	for index, rawURL := range []string{"https://example.com/a", "https://example.com/b"} {
		if err := store.Upsert(context.Background(), searchindex.Page{
			URL: rawURL, Root: "example.com", Title: "Same release notes",
			Body: "same release notes version package details", Lang: searchindex.LangEnglish,
			FetchedAt: time.Unix(1_700_000_000+int64(index), 0).UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	service, _ := New(store)
	request := &models.SearchRequest{Query: "  release   notes  "}
	response, err := service.Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if request.Query != "  release   notes  " || request.Limit != 0 || request.Deduplicate != nil {
		t.Fatalf("request mutated: %#v", request)
	}
	if response.Query != "release notes" || len(response.Results) != 1 || response.Deduplicated != 1 {
		t.Fatalf("response = %#v", response)
	}
}

func TestSearchAttachesRawObservation(t *testing.T) {
	store := openStore(t)
	pages := []searchindex.Page{
		{URL: "https://example.com/promise", Root: "example.com", Title: "Promise reference", Body: "Promise resolve reject asynchronous JavaScript reference", Lang: searchindex.LangEnglish},
		{URL: "https://example.com/promise-tutorial", Root: "example.com", Title: "Promise tutorial", Body: "Promise asynchronous JavaScript tutorial", Lang: searchindex.LangEnglish},
		{URL: "https://other.example.net/promise", Root: "example.net", Title: "Promise guide", Body: "Promise JavaScript guide overview", Lang: searchindex.LangEnglish},
	}
	for index, page := range pages {
		page.FetchedAt = time.Unix(1_700_000_000+int64(index), 0).UTC()
		if err := store.Upsert(context.Background(), page); err != nil {
			t.Fatal(err)
		}
	}
	service, _ := New(store)
	response, err := service.Search(context.Background(), &models.SearchRequest{
		Query: "Promise JavaScript", Domains: []string{"example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	observation := response.Observation
	if observation == nil {
		t.Fatal("response carries no observation")
	}
	if observation.QueryLang != searchindex.LangEnglish {
		t.Fatalf("query lang = %q", observation.QueryLang)
	}
	// Three index candidates existed; the domain scope removed the example.net
	// one, and both survivors were served with their raw scores.
	if observation.CandidateCount != 3 || observation.DomainFiltered != 1 {
		t.Fatalf("observation = %+v", observation)
	}
	if observation.ScoreGap == nil {
		t.Fatal("score gap missing with two candidates")
	}
	if len(observation.Served) != len(response.Results) {
		t.Fatalf("served %d != results %d", len(observation.Served), len(response.Results))
	}
	for index, served := range observation.Served {
		if served.URL != response.Results[index].URL {
			t.Fatalf("served order diverges at %d: %s != %s", index, served.URL, response.Results[index].URL)
		}
		if served.Score == 0 || served.Lang == "" {
			t.Fatalf("served missing raw fields: %+v", served)
		}
	}
	// The observation must never serialize into a customer-visible response.
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Observation", "CandidateCount", "ScoreGap", "QueryLang", "Served"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("serialized response leaks %q: %s", forbidden, encoded)
		}
	}
}

func openStore(t *testing.T) *searchindex.Store {
	t.Helper()
	store, err := searchindex.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
