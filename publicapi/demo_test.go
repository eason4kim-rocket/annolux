package publicapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (fixture serverFixture) demoRequest(t *testing.T, body, clientIP string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/demo/search", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if clientIP != "" {
		request.Header.Set("CF-Connecting-IP", clientIP)
	}
	recorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestDemoSearchNeedsNoAuthMovesNoCreditsAndClampsCeilings(t *testing.T) {
	fixture := newServerFixture(t)
	response := fixture.demoRequest(t, `{"query":"rust ownership","limit":10,"timeout":30}`, "203.0.113.7")
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d body=%s", response.Code, response.Body.String())
	}
	if used := response.Header().Get(CreditsUsedHeader); used != "" {
		t.Fatalf("demo response advertised credits used = %q", used)
	}
	if left := response.Header().Get(CreditsLeftHeader); left != "" {
		t.Fatalf("demo response advertised credits remaining = %q", left)
	}
	var envelope struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || !envelope.Success {
		t.Fatalf("body = %s, %v", response.Body.String(), err)
	}
	if len(fixture.search.requests) != 1 || fixture.search.requests[0].Limit != demoMaxLimit || fixture.search.requests[0].Timeout != demoMaxTimeout {
		t.Fatalf("engine requests = %#v", fixture.search.requests)
	}
	summary, err := fixture.store.CreditSummary(t.Context(), fixture.account.ID)
	if err != nil || summary.Used != 0 {
		t.Fatalf("credits moved: summary = %#v, %v", summary, err)
	}
}

func TestDemoSearchRateLimitsPerClientIP(t *testing.T) {
	fixture := newServerFixture(t)
	for index := 0; index < 5; index++ {
		if response := fixture.demoRequest(t, `{"query":"rust ownership"}`, "203.0.113.9"); response.Code != http.StatusOK {
			t.Fatalf("request %d = %d body=%s", index, response.Code, response.Body.String())
		}
	}
	blocked := fixture.demoRequest(t, `{"query":"rust ownership"}`, "203.0.113.9")
	if blocked.Code != http.StatusTooManyRequests || !strings.Contains(blocked.Body.String(), "RATE_LIMITED") {
		t.Fatalf("sixth request = %d body=%s", blocked.Code, blocked.Body.String())
	}
	other := fixture.demoRequest(t, `{"query":"rust ownership"}`, "198.51.100.4")
	if other.Code != http.StatusOK {
		t.Fatalf("other client = %d body=%s", other.Code, other.Body.String())
	}
}

func TestDemoSearchGlobalDailyQuotaResetsAtUTCMidnight(t *testing.T) {
	quota := newDailyQuota(2)
	current := time.Date(2026, 8, 20, 23, 0, 0, 0, time.UTC)
	quota.now = func() time.Time { return current }
	if !quota.Allow() || !quota.Allow() {
		t.Fatal("quota rejected within budget")
	}
	if quota.Allow() {
		t.Fatal("quota allowed beyond budget")
	}
	current = current.Add(2 * time.Hour)
	if !quota.Allow() {
		t.Fatal("quota did not reset on the next UTC day")
	}
}

func TestDemoSearchRejectsFreshnessLikeStandardSearch(t *testing.T) {
	fixture := newServerFixture(t)
	response := fixture.demoRequest(t, `{"query":"rust ownership","freshness":"day"}`, "203.0.113.11")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "UNSUPPORTED_OPTION") {
		t.Fatalf("response = %d body=%s", response.Code, response.Body.String())
	}
	if len(fixture.search.requests) != 0 {
		t.Fatalf("invalid request reached the engine: %#v", fixture.search.requests)
	}
}
