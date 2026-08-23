package publicapi

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/eason4kim-rocket/annolux/models"
)

// Demo search is the homepage try-it endpoint: the standard Search contract
// with no authentication and no credit movement. Abuse is bounded three ways:
// a per-IP limiter, a global daily quota, and tighter ceilings on limit and
// timeout. Requests still produce query-log records under source "demo" so
// they can be weighed separately from paying traffic.
const (
	demoMaxLimit    = 3
	demoMaxTimeout  = 10
	demoDailyBudget = 2000
)

// dailyQuota is a coarse global cap that resets at UTC midnight.
type dailyQuota struct {
	mu    sync.Mutex
	day   string
	used  int
	limit int
	now   func() time.Time
}

func newDailyQuota(limit int) *dailyQuota {
	return &dailyQuota{limit: limit, now: time.Now}
}

func (quota *dailyQuota) Allow() bool {
	day := quota.now().UTC().Format(time.DateOnly)
	quota.mu.Lock()
	defer quota.mu.Unlock()
	if quota.day != day {
		quota.day, quota.used = day, 0
	}
	if quota.used >= quota.limit {
		return false
	}
	quota.used++
	return true
}

// demoClientIP prefers the Cloudflare-set header: the API is reachable only
// through the tunnel, so external callers cannot forge it.
func demoClientIP(request *http.Request) string {
	if ip := strings.TrimSpace(request.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

func (server *Server) handleDemoSearch(writer http.ResponseWriter, request *http.Request) {
	publicRequest, code, message, ok := decodePublicSearchRequest(writer, request)
	if !ok {
		writeSearchError(writer, http.StatusBadRequest, code, message)
		return
	}
	// The demo serves the identical contract with tighter ceilings; requests
	// asking for more are clamped rather than rejected.
	if publicRequest.Limit == 0 || publicRequest.Limit > demoMaxLimit {
		publicRequest.Limit = demoMaxLimit
	}
	if publicRequest.Timeout == 0 || publicRequest.Timeout > demoMaxTimeout {
		publicRequest.Timeout = demoMaxTimeout
	}
	if !server.demoLimiter.Allow(demoClientIP(request)) {
		writeSearchError(writer, http.StatusTooManyRequests, "RATE_LIMITED", "demo rate limit exceeded; create a free account for the full API")
		return
	}
	if !server.demoQuota.Allow() {
		writeSearchError(writer, http.StatusTooManyRequests, "CAPACITY_LIMITED", "the demo is out of capacity for today; create a free account for the full API")
		return
	}
	select {
	case server.gate <- struct{}{}:
		defer func() { <-server.gate }()
	default:
		writeSearchError(writer, http.StatusTooManyRequests, "CAPACITY_LIMITED", "search capacity is temporarily full")
		return
	}
	requestID := writer.Header().Get(RequestIDHeader)
	started := time.Now()
	response, status, errCode, errMessage := server.executeSearch(request.Context(), publicRequest)
	elapsed := time.Since(started)
	if status != http.StatusOK {
		server.logSearch(request, "demo", "", requestID, publicRequest,
			searchOutcome{status: status, errorCode: errCode, elapsed: elapsed})
		writeSearchError(writer, status, errCode, errMessage)
		return
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > models.MaxSearchResponseBytes {
		server.logSearch(request, "demo", "", requestID, publicRequest,
			searchOutcome{status: http.StatusInternalServerError, errorCode: "INTERNAL_ERROR", response: response, observation: response.observation, elapsed: elapsed})
		writeSearchError(writer, http.StatusInternalServerError, "INTERNAL_ERROR", "search response could not be encoded")
		return
	}
	server.logSearch(request, "demo", "", requestID, publicRequest,
		searchOutcome{status: http.StatusOK, response: response, observation: response.observation, elapsed: elapsed})
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(encoded)
}
