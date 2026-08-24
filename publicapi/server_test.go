package publicapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eason4kim-rocket/annolux/accounts"
	"github.com/eason4kim-rocket/annolux/models"
)

type fakeSearchService struct {
	err      error
	response *models.SearchResponse
	requests []*models.SearchRequest
}

func (service *fakeSearchService) Search(_ context.Context, request *models.SearchRequest) (*models.SearchResponse, error) {
	copy := *request
	copy.Domains = append([]string(nil), request.Domains...)
	service.requests = append(service.requests, &copy)
	if service.err != nil {
		return nil, service.err
	}
	if service.response != nil {
		return service.response, nil
	}
	return &models.SearchResponse{Success: true, Query: strings.Join(strings.Fields(request.Query), " "), Results: []models.SearchResult{}, Timing: models.SearchTimingInfo{}}, nil
}

type serverFixture struct {
	server       *Server
	store        *accounts.Store
	account      accounts.Account
	apiKey       accounts.APIKey
	apiSecret    string
	session      accounts.Session
	sessionToken string
	csrf         string
	search       *fakeSearchService
}

func newServerFixture(t *testing.T) serverFixture {
	t.Helper()
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	store, err := accounts.Open(context.Background(), filepath.Join(t.TempDir(), "accounts.db"), accounts.Config{
		HMACSecret: []byte("0123456789abcdef0123456789abcdef"), Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	account, created, err := store.LoginIdentity(context.Background(), accounts.IdentityInput{Provider: "google", Subject: "test-subject", Email: "test@example.com"})
	if err != nil || !created {
		t.Fatal(err)
	}
	key, secret, err := store.CreateAPIKey(context.Background(), account.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	session, sessionToken, csrf, err := store.CreateSession(context.Background(), account.ID, accounts.DefaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	searchService := &fakeSearchService{}
	server, err := New(Config{Origin: "http://127.0.0.1:8080", Accounts: store, Search: searchService, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return serverFixture{server: server, store: store, account: account, apiKey: key, apiSecret: secret, session: session, sessionToken: sessionToken, csrf: csrf, search: searchService}
}

func (fixture serverFixture) searchRequest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/search", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+fixture.apiSecret)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(RequestIDHeader, "client-reused-request-id")
	recorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestCommercialSearchChargesExactlyOneOnlyOnSuccess(t *testing.T) {
	fixture := newServerFixture(t)
	first := fixture.searchRequest(t, `{"query":"rust ownership","limit":5,"deduplicate":true,"timeout":10}`)
	if first.Code != http.StatusOK || first.Header().Get(CreditsUsedHeader) != "1" || first.Header().Get(CreditsLeftHeader) != "999" || first.Header().Get(RequestIDHeader) == "client-reused-request-id" {
		t.Fatalf("first response = %d headers=%v body=%s", first.Code, first.Header(), first.Body.String())
	}
	second := fixture.searchRequest(t, `{"query":"rust ownership"}`)
	if second.Code != http.StatusOK || second.Header().Get(CreditsLeftHeader) != "998" || second.Header().Get(RequestIDHeader) == first.Header().Get(RequestIDHeader) {
		t.Fatalf("second response = %d headers=%v body=%s", second.Code, second.Header(), second.Body.String())
	}
	summary, err := fixture.store.CreditSummary(context.Background(), fixture.account.ID)
	if err != nil || summary.Used != 2 || summary.Available != 998 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
	if len(fixture.search.requests) != 2 || fixture.search.requests[0].Limit != 5 || fixture.search.requests[0].Timeout != 10 || fixture.search.requests[0].Ranking != models.SearchRankingProvider {
		t.Fatalf("requests = %#v", fixture.search.requests)
	}
}

func TestAllAPIKeysShareOneAccountRateLimit(t *testing.T) {
	fixture := newServerFixture(t)
	_, secondSecret, err := fixture.store.CreateAPIKey(context.Background(), fixture.account.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	fixture.server.limiter = newKeyLimiters(0.000001, 10)
	requestWith := func(secret string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/search", strings.NewReader(`{"query":"shared account limit"}`))
		request.Header.Set("Authorization", "Bearer "+secret)
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		fixture.server.Handler().ServeHTTP(recorder, request)
		return recorder
	}
	for index := 0; index < 10; index++ {
		secret := fixture.apiSecret
		if index%2 == 1 {
			secret = secondSecret
		}
		if response := requestWith(secret); response.Code != http.StatusOK {
			t.Fatalf("request %d = %d %s", index, response.Code, response.Body.String())
		}
	}
	if response := requestWith(secondSecret); response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), `"code":"RATE_LIMITED"`) {
		t.Fatalf("eleventh request = %d %s", response.Code, response.Body.String())
	}
	summary, err := fixture.store.CreditSummary(context.Background(), fixture.account.ID)
	if err != nil || summary.Used != 10 || summary.Available != accounts.FreeMonthlyCredits-10 || summary.Reserved != 0 {
		t.Fatalf("rate-limited summary = %#v, %v", summary, err)
	}
}

func TestCommercialSearchRejectsUnsupportedAndUnknownFieldsWithoutCharge(t *testing.T) {
	fixture := newServerFixture(t)
	for _, test := range []struct {
		body string
		code string
	}{
		{`{"query":"news","freshness":"day"}`, "UNSUPPORTED_OPTION"},
		{`{"query":"news","ranking":"trust"}`, "UNSUPPORTED_OPTION"},
		{`{"query":"news","include_content":true}`, "INVALID_INPUT"},
		{`{"query":"news","limit":11}`, "INVALID_INPUT"},
	} {
		response := fixture.searchRequest(t, test.body)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
			t.Fatalf("response(%s) = %d %s", test.body, response.Code, response.Body.String())
		}
	}
	summary, err := fixture.store.CreditSummary(context.Background(), fixture.account.ID)
	if err != nil || summary.Available != accounts.FreeMonthlyCredits || summary.Used != 0 || len(fixture.search.requests) != 0 {
		t.Fatalf("summary = %#v, requests=%d, err=%v", summary, len(fixture.search.requests), err)
	}
}

func TestCommercialSearchFailureReleasesReservation(t *testing.T) {
	fixture := newServerFixture(t)
	fixture.search.err = models.NewServiceError(models.ErrCodeSearchFailed, "provider failed", errors.New("private upstream detail"))
	response := fixture.searchRequest(t, `{"query":"failure"}`)
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "private upstream") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	summary, err := fixture.store.CreditSummary(context.Background(), fixture.account.ID)
	if err != nil || summary.Available != accounts.FreeMonthlyCredits || summary.Used != 0 || summary.Reserved != 0 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
}

func TestCommercialSearchRejectsResultWithoutFetchTimeAndReleasesReservation(t *testing.T) {
	fixture := newServerFixture(t)
	fixture.search.response = &models.SearchResponse{
		Success: true, Query: "freshness invariant",
		Results: []models.SearchResult{{Rank: 1, Title: "missing time", URL: "https://example.com/"}},
	}
	response := fixture.searchRequest(t, `{"query":"freshness invariant"}`)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	summary, err := fixture.store.CreditSummary(context.Background(), fixture.account.ID)
	if err != nil || summary.Available != accounts.FreeMonthlyCredits || summary.Used != 0 || summary.Reserved != 0 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
}

func TestCommercialSearchTimeoutReleasesReservation(t *testing.T) {
	fixture := newServerFixture(t)
	fixture.search.err = context.DeadlineExceeded
	response := fixture.searchRequest(t, `{"query":"slow query","timeout":1}`)
	if response.Code != http.StatusGatewayTimeout || !strings.Contains(response.Body.String(), `"code":"SEARCH_TIMEOUT"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	summary, err := fixture.store.CreditSummary(context.Background(), fixture.account.ID)
	if err != nil || summary.Available != accounts.FreeMonthlyCredits || summary.Used != 0 || summary.Reserved != 0 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
}

func TestCommercialSearchAuthenticationFailuresNeverCharge(t *testing.T) {
	fixture := newServerFixture(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/search", strings.NewReader(`{"query":"unauthorized"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized response = %d %s", recorder.Code, recorder.Body.String())
	}
	if err := fixture.store.SetAccountStatus(context.Background(), fixture.account.ID, accounts.AccountSuspended); err != nil {
		t.Fatal(err)
	}
	suspended := fixture.searchRequest(t, `{"query":"suspended"}`)
	if suspended.Code != http.StatusUnauthorized && suspended.Code != http.StatusForbidden {
		t.Fatalf("suspended response = %d %s", suspended.Code, suspended.Body.String())
	}
	summary, err := fixture.store.CreditSummary(context.Background(), fixture.account.ID)
	if err != nil || summary.Available != accounts.FreeMonthlyCredits || summary.Used != 0 || summary.Reserved != 0 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
}

func TestAccountWritesRequireExactOriginAndCSRF(t *testing.T) {
	fixture := newServerFixture(t)
	makeRequest := func(origin, csrf string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/account/api-keys", bytes.NewBufferString(`{"name":"second"}`))
		request.Header.Set("Origin", origin)
		request.Header.Set("X-CSRF-Token", csrf)
		request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: fixture.sessionToken})
		recorder := httptest.NewRecorder()
		fixture.server.Handler().ServeHTTP(recorder, request)
		return recorder
	}
	if response := makeRequest("https://evil.example", fixture.csrf); response.Code != http.StatusForbidden {
		t.Fatalf("evil origin = %d %s", response.Code, response.Body.String())
	}
	if response := makeRequest("http://127.0.0.1:8080", "wrong"); response.Code != http.StatusForbidden {
		t.Fatalf("wrong CSRF = %d %s", response.Code, response.Body.String())
	}
	response := makeRequest("http://127.0.0.1:8080", fixture.csrf)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"shown_once":true`) || !strings.Contains(response.Body.String(), accounts.APIKeyPrefix) {
		t.Fatalf("valid write = %d %s", response.Code, response.Body.String())
	}
}

func TestReadyChecksAccountsAndSearchReadiness(t *testing.T) {
	fixture := newServerFixture(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)
	recorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("ready = %d %s", recorder.Code, recorder.Body.String())
	}
}

func decodeErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope errorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Error.Code
}
