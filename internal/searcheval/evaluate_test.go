package searcheval

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eason4kim-rocket/annolux/models"
)

func TestEvaluateRecordsStrongAcceptableAndMissWithoutTreatingUnjudgedAsIrrelevant(t *testing.T) {
	responses := map[string][]models.SearchResult{
		"strong query": {
			{Rank: 1, Title: "Primary", URL: "http://www.example.com/docs/"},
			{Rank: 2, Title: "Unjudged", URL: "https://other.example/page"},
		},
		"acceptable query": {
			{Rank: 1, Title: "Unjudged", URL: "https://other.example/page"},
			{Rank: 2, Title: "Primary", URL: "https://example.com/acceptable"},
		},
		"miss query": {
			{Rank: 1, Title: "Unjudged", URL: "https://other.example/page"},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/search" {
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer ann_live_test-secret" {
			t.Error("request omitted API credential")
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(writer).Encode(models.SearchResponse{
			Success: true, Query: payload.Query, Results: responses[payload.Query],
			Timing: models.SearchTimingInfo{TotalMs: 7, ProviderMs: 7},
		})
	}))
	defer server.Close()

	corpus := Corpus{
		Manifest: Manifest{Name: "test-corpus", FrozenAt: "2026-08-15", Protocol: Protocol{Limit: 10, StrongGrade: 3, RelevantGrade: 2}},
		Digest:   "abc123",
		Cases: []Case{
			{ID: "strong-case", Split: "dev", Language: "en", Category: "technical", Query: "strong query", Targets: []Target{{URL: "https://example.com/docs", Relevance: 3}}},
			{ID: "acceptable-case", Split: "heldout", Language: "en", Category: "reference", Query: "acceptable query", Targets: []Target{{URL: "https://example.com/acceptable", Relevance: 3}}},
			{ID: "miss-case", Split: "heldout", Language: "zh", Category: "news", Query: "miss query", Targets: []Target{{URL: "https://example.com/missing", Relevance: 3}}},
		},
	}
	report, err := Evaluate(context.Background(), server.Client(), corpus, RunOptions{
		RunID: "round-test", APIURL: server.URL, APIKey: "ann_live_test-secret", CodeCommit: "deadbeef",
		EvaluatorCommit: "feedface", ImageDigest: "sha256:cafe",
		Split: "all", Timeout: time.Second,
		Index: IndexIdentity{Pages: 2, EnglishPages: 1, ChinesePages: 1, LatestFetchedAt: 1, DatabaseBytes: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{report.Cases[0].Verdict, report.Cases[1].Verdict, report.Cases[2].Verdict}; got[0] != "strong" || got[1] != "acceptable" || got[2] != "miss" {
		t.Fatalf("verdicts = %v", got)
	}
	if report.Cases[0].Results[1].Grade != 0 {
		t.Fatal("unjudged result was assigned relevance")
	}
	if report.Summary.Overall.Strong != 1 || report.Summary.Overall.Acceptable != 1 || report.Summary.Overall.Miss != 1 {
		t.Fatalf("summary = %#v", report.Summary.Overall)
	}
	if report.Summary.ClientLatencySamples != 3 || report.Summary.ServerLatencySamples != 3 ||
		report.Summary.ServerMedianLatencyMS != 7 {
		t.Fatalf("latency summary = %#v", report.Summary)
	}
	if report.Metadata.RequestTimeoutMS != 1000 {
		t.Fatalf("request timeout = %d", report.Metadata.RequestTimeoutMS)
	}
	encoded, err := EncodeJSONL(report)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("test-secret")) {
		t.Fatal("evaluation output contains API credential")
	}
}

func TestEvaluateRecordsHTTPFailureAndContinues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	corpus := Corpus{
		Manifest: Manifest{Name: "test-corpus", FrozenAt: "2026-08-15", Protocol: Protocol{Limit: 10, StrongGrade: 3, RelevantGrade: 2}},
		Digest:   "abc123",
		Cases: []Case{{ID: "failed-case", Split: "dev", Language: "en", Category: "technical", Query: "failed query",
			Targets: []Target{{URL: "https://example.com/target", Relevance: 3}}}},
	}
	report, err := Evaluate(context.Background(), server.Client(), corpus, RunOptions{
		RunID: "round-failure", APIURL: server.URL, APIKey: "ann_live_test-secret", CodeCommit: "deadbeef",
		EvaluatorCommit: "feedface", ImageDigest: "sha256:cafe",
		Split: "all", Timeout: time.Second,
		Index: IndexIdentity{Pages: 1, EnglishPages: 1, LatestFetchedAt: 1, DatabaseBytes: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Cases[0].Error != "http_503" || report.Summary.Overall.Errors != 1 {
		t.Fatalf("failure report = %#v / %#v", report.Cases[0], report.Summary.Overall)
	}
	if report.Summary.ServerLatencySamples != 0 {
		t.Fatalf("failed response contributed a server latency: %#v", report.Summary)
	}
}
