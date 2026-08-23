package publicapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eason4kim-rocket/annolux/models"
	"github.com/eason4kim-rocket/annolux/querylog"
)

// observedResponse is what the fake engine serves: two results plus the raw
// observation hostedsearch would attach.
func observedResponse() *models.SearchResponse {
	fetched := time.Date(2026, 8, 13, 11, 0, 0, 0, time.UTC)
	gap := -0.7
	return &models.SearchResponse{
		Success: true, Query: "kubernetes ingress",
		Results: []models.SearchResult{
			{Rank: 1, Title: "Ingress", URL: "https://kubernetes.io/docs/concepts/ingress/", FetchedAt: &fetched},
			{Rank: 2, Title: "Ingress Controllers", URL: "https://kubernetes.io/docs/concepts/ingress-controllers/", FetchedAt: &fetched},
		},
		Deduplicated: 3, Timing: models.SearchTimingInfo{TotalMs: 45, ProviderMs: 40},
		Observation: &models.SearchObservation{
			QueryLang: "en", CandidateCount: 20, ScoreGap: &gap,
			DomainFiltered: 2, NearDupRemoved: 1, OtherLangHits: 0,
			Served: []models.ServedDocument{
				{URL: "https://kubernetes.io/docs/concepts/ingress/", Lang: "en", Score: -5.1},
				{URL: "https://kubernetes.io/docs/concepts/ingress-controllers/", Lang: "en", Score: -4.4},
			},
		},
	}
}

type loggedFixture struct {
	serverFixture
	logger  *querylog.Logger
	logPath string
}

func newLoggedFixture(t *testing.T) loggedFixture {
	t.Helper()
	base := newServerFixture(t)
	logPath := filepath.Join(t.TempDir(), "querylog.db")
	store, err := querylog.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logger := querylog.NewLogger(store, nil, querylog.DefaultRetention)
	t.Cleanup(func() { _ = logger.Close() })
	base.server.querylog = logger
	return loggedFixture{serverFixture: base, logger: logger, logPath: logPath}
}

type loggedRow struct {
	RequestID  string
	Source     string
	AccountID  string
	QueryText  string
	QueryLang  string
	Domains    string
	HTTPStatus int
	ErrorCode  string
	Results    int
	Zero       int
	TopDocs    string
	Candidates int
	Gap        sql.NullFloat64
	Signals    string
	Ranker     string
	LatencyMS  int64
}

func (fixture loggedFixture) rows(t *testing.T) []loggedRow {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fixture.logger.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", fixture.logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	queried, err := db.Query(`SELECT request_id, source, account_id, query_text, query_lang, domains,
		http_status, error_code, result_count, zero_result, top_docs, candidate_count,
		score_gap_top1_top2, signals, ranker_version, latency_ms FROM query_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer queried.Close()
	var rows []loggedRow
	for queried.Next() {
		var row loggedRow
		if err := queried.Scan(&row.RequestID, &row.Source, &row.AccountID, &row.QueryText, &row.QueryLang,
			&row.Domains, &row.HTTPStatus, &row.ErrorCode, &row.Results, &row.Zero, &row.TopDocs,
			&row.Candidates, &row.Gap, &row.Signals, &row.Ranker, &row.LatencyMS); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	return rows
}

func TestSearchSuccessIsLoggedWithObservation(t *testing.T) {
	fixture := newLoggedFixture(t)
	fixture.search.response = observedResponse()
	response := fixture.searchRequest(t, `{"query":"kubernetes ingress","domains":["kubernetes.io"]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("search = %d %s", response.Code, response.Body.String())
	}
	rows := fixture.rows(t)
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	row := rows[0]
	if row.RequestID != response.Header().Get(RequestIDHeader) {
		t.Fatalf("request id %q != header %q", row.RequestID, response.Header().Get(RequestIDHeader))
	}
	if row.Source != "api" || row.AccountID != fixture.account.ID || row.HTTPStatus != 200 || row.ErrorCode != "" {
		t.Fatalf("row = %+v", row)
	}
	if row.QueryText != "kubernetes ingress" || row.QueryLang != "en" || row.Domains != `["kubernetes.io"]` {
		t.Fatalf("row = %+v", row)
	}
	if row.Results != 2 || row.Zero != 0 || row.Candidates != 20 || !row.Gap.Valid || row.Gap.Float64 != -0.7 {
		t.Fatalf("row = %+v", row)
	}
	if row.Ranker != "test" || row.LatencyMS < 0 {
		t.Fatalf("row = %+v", row)
	}
	var docs []querylog.TopDoc
	if err := json.Unmarshal([]byte(row.TopDocs), &docs); err != nil || len(docs) != 2 {
		t.Fatalf("top_docs = %s, %v", row.TopDocs, err)
	}
	// The logged ranking must match the served response order exactly.
	if docs[0].Rank != 1 || docs[0].URLHash != querylog.HashURL("https://kubernetes.io/docs/concepts/ingress/") ||
		docs[1].Rank != 2 || docs[1].URLHash != querylog.HashURL("https://kubernetes.io/docs/concepts/ingress-controllers/") ||
		docs[0].Score != -5.1 || docs[0].Lang != "en" {
		t.Fatalf("top_docs = %+v", docs)
	}
	var signals querylog.Signals
	if err := json.Unmarshal([]byte(row.Signals), &signals); err != nil ||
		signals.DomainFiltered != 2 || signals.NearDupRemoved != 1 || signals.Deduplicated != 3 ||
		signals.ProviderMs != 40 || signals.TotalMs != 45 {
		t.Fatalf("signals = %s, %v", row.Signals, err)
	}
}

func TestZeroResultSearchIsMarked(t *testing.T) {
	fixture := newLoggedFixture(t)
	response := fixture.searchRequest(t, `{"query":"no results at all"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("search = %d %s", response.Code, response.Body.String())
	}
	rows := fixture.rows(t)
	if len(rows) != 1 || rows[0].Zero != 1 || rows[0].Results != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestEngineFailureIsLoggedAuthFailureIsNot(t *testing.T) {
	fixture := newLoggedFixture(t)
	fixture.search.err = context.DeadlineExceeded
	response := fixture.searchRequest(t, `{"query":"slow query"}`)
	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("search = %d", response.Code)
	}
	unauthenticated := httptest.NewRequest(http.MethodPost, "/api/v1/search", strings.NewReader(`{"query":"secret intent"}`))
	unauthenticated.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(recorder, unauthenticated)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized = %d", recorder.Code)
	}
	rows := fixture.rows(t)
	if len(rows) != 1 || rows[0].HTTPStatus != http.StatusGatewayTimeout || rows[0].ErrorCode != "SEARCH_TIMEOUT" || rows[0].Zero != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestOpsMarkerSkipsLogging(t *testing.T) {
	fixture := newLoggedFixture(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/search", strings.NewReader(`{"query":"smoke probe"}`))
	request.Header.Set("Authorization", "Bearer "+fixture.apiSecret)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(OpsMarkerHeader, "probe")
	recorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("search = %d %s", recorder.Code, recorder.Body.String())
	}
	if rows := fixture.rows(t); len(rows) != 0 {
		t.Fatalf("ops traffic was logged: %+v", rows)
	}
}

func TestPlaygroundSearchIsLoggedWithSource(t *testing.T) {
	fixture := newLoggedFixture(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/playground/search", strings.NewReader(`{"query":"playground query"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	request.Header.Set("X-CSRF-Token", fixture.csrf)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: fixture.sessionToken})
	recorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("playground = %d %s", recorder.Code, recorder.Body.String())
	}
	rows := fixture.rows(t)
	if len(rows) != 1 || rows[0].Source != "playground" || rows[0].AccountID != fixture.account.ID {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestLogFileHoldsNoCredentialsOrClientMetadata(t *testing.T) {
	fixture := newLoggedFixture(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/search", strings.NewReader(`{"query":"privacy scan"}`))
	request.Header.Set("Authorization", "Bearer "+fixture.apiSecret)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "sentinel-user-agent/9.9")
	request.RemoteAddr = "203.0.113.77:4242"
	recorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("search = %d", recorder.Code)
	}
	if rows := fixture.rows(t); len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	raw, err := os.ReadFile(fixture.logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{fixture.apiSecret, "sentinel-user-agent", "203.0.113.77", "Bearer"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("log file contains forbidden value %q", forbidden)
		}
	}
}

func TestNilLoggerDisablesLoggingWithoutError(t *testing.T) {
	fixture := newServerFixture(t)
	response := fixture.searchRequest(t, `{"query":"no logger configured"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("search = %d %s", response.Code, response.Body.String())
	}
}
