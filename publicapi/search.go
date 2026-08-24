package publicapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eason4kim-rocket/annolux/accounts"
	"github.com/eason4kim-rocket/annolux/models"
)

const (
	maximumPublicSearchBody = 64 << 10
	maximumPublicLimit      = 10
	maximumPublicTimeout    = 30
)

type publicSearchRequest struct {
	Query       string           `json:"query"`
	Domains     []string         `json:"domains,omitempty"`
	Deduplicate *bool            `json:"deduplicate,omitempty"`
	Limit       int              `json:"limit,omitempty"`
	Timeout     int              `json:"timeout,omitempty"`
	Ranking     string           `json:"ranking,omitempty"`
	Freshness   *json.RawMessage `json:"freshness,omitempty"`
}

type searchErrorEnvelope struct {
	Success bool                 `json:"success"`
	Results []publicSearchResult `json:"results"`
	Error   errorDetail          `json:"error"`
}

// Public response types are deliberately separate from the internal search
// models so Labs evidence, extraction, trust, and receipt fields can never
// leak into the commercial Search contract by accident.
type publicSearchResult struct {
	Rank      int        `json:"rank"`
	Score     *float64   `json:"score,omitempty"`
	Title     string     `json:"title"`
	URL       string     `json:"url"`
	Snippet   string     `json:"snippet,omitempty"`
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
}

type publicSearchResponse struct {
	Success      bool                    `json:"success"`
	Query        string                  `json:"query"`
	Results      []publicSearchResult    `json:"results"`
	Deduplicated int                     `json:"deduplicated"`
	Partial      bool                    `json:"partial"`
	Timing       models.SearchTimingInfo `json:"timing"`

	// observation is unexported so raw ranking features can never serialize
	// into the public response; it exists only to feed the query log.
	observation *models.SearchObservation
}

// ensureMonthlyFreeCredits tops up the account's current-month free-tier credits
// before a credit-bearing operation. It is best-effort: a failure is logged but
// never blocks the request, because the credit reservation is the authoritative
// balance check.
func (server *Server) ensureMonthlyFreeCredits(ctx context.Context, accountID string) {
	if _, _, err := server.accounts.EnsureMonthlyFreeCredits(ctx, accountID); err != nil {
		server.logger.Warn("monthly free credit top-up failed", "account_id", accountID, "error", err)
	}
}

func (server *Server) handleSearch(writer http.ResponseWriter, request *http.Request) {
	authenticated, ok := server.requireAPIKey(writer, request)
	if !ok {
		return
	}
	publicRequest, code, message, ok := decodePublicSearchRequest(writer, request)
	if !ok {
		writeSearchError(writer, http.StatusBadRequest, code, message)
		return
	}
	// All keys on one account share the same plan limit; creating three keys
	// must not multiply the account's throughput entitlement.
	if !server.limiter.Allow(authenticated.AccountID) {
		writeSearchError(writer, http.StatusTooManyRequests, "RATE_LIMITED", "rate limit exceeded")
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
	server.ensureMonthlyFreeCredits(request.Context(), authenticated.AccountID)
	reservation, _, err := server.accounts.ReserveCredits(request.Context(), authenticated.AccountID, authenticated.KeyID, requestID, 1)
	if errors.Is(err, accounts.ErrInsufficientCredits) {
		writeSearchError(writer, http.StatusPaymentRequired, "INSUFFICIENT_CREDITS", "credit balance is empty")
		return
	}
	if err != nil {
		server.logger.Error("credit reservation failed", "request_id", requestID, "error", err)
		writeSearchError(writer, http.StatusServiceUnavailable, "BILLING_UNAVAILABLE", "credit ledger is unavailable")
		return
	}
	committed := false
	defer func() {
		if !committed {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, releaseErr := server.accounts.ReleaseReservation(ctx, reservation.ID); releaseErr != nil {
				server.logger.Error("credit release failed", "request_id", requestID, "reservation_id", reservation.ID, "error", releaseErr)
			}
		}
	}()

	committed = server.finishSearch(writer, request, "api", authenticated.AccountID, requestID, publicRequest,
		func(settleContext context.Context) (int64, error) {
			remaining, commitErr := server.accounts.CommitReservation(settleContext, reservation.ID)
			if commitErr != nil {
				server.logger.Error("credit commit failed", "request_id", requestID, "reservation_id", reservation.ID, "error", commitErr)
			}
			return remaining, commitErr
		})
}

// finishSearch runs the shared tail of both Search surfaces: engine call,
// response encoding, credit settlement, and exactly one query-log record for
// every request that reached the engine — successes and engine failures
// alike. It reports whether the reservation was committed.
func (server *Server) finishSearch(
	writer http.ResponseWriter, request *http.Request,
	source, accountID, requestID string, publicRequest publicSearchRequest,
	commit func(context.Context) (int64, error),
) bool {
	started := time.Now()
	response, status, code, message := server.executeSearch(request.Context(), publicRequest)
	elapsed := time.Since(started)
	if status != http.StatusOK {
		server.logSearch(request, source, accountID, requestID, publicRequest,
			searchOutcome{status: status, errorCode: code, elapsed: elapsed})
		writeSearchError(writer, status, code, message)
		return false
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > models.MaxSearchResponseBytes {
		server.logSearch(request, source, accountID, requestID, publicRequest,
			searchOutcome{status: http.StatusInternalServerError, errorCode: "INTERNAL_ERROR", response: response, observation: response.observation, elapsed: elapsed})
		writeSearchError(writer, http.StatusInternalServerError, "INTERNAL_ERROR", "search response could not be encoded")
		return false
	}
	settleContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	remaining, err := commit(settleContext)
	cancel()
	if err != nil {
		server.logSearch(request, source, accountID, requestID, publicRequest,
			searchOutcome{status: http.StatusServiceUnavailable, errorCode: "BILLING_UNAVAILABLE", response: response, observation: response.observation, elapsed: elapsed})
		writeSearchError(writer, http.StatusServiceUnavailable, "BILLING_UNAVAILABLE", "credit ledger is unavailable")
		return false
	}
	server.logSearch(request, source, accountID, requestID, publicRequest,
		searchOutcome{status: http.StatusOK, response: response, observation: response.observation, elapsed: elapsed})
	writer.Header().Set(CreditsUsedHeader, "1")
	writer.Header().Set(CreditsLeftHeader, strconv.FormatInt(remaining, 10))
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(encoded)
	return true
}

func (server *Server) executeSearch(parent context.Context, publicRequest publicSearchRequest) (*publicSearchResponse, int, string, string) {
	timeout := publicRequest.Timeout
	if timeout == 0 {
		timeout = maximumPublicTimeout
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeout)*time.Second)
	defer cancel()
	deduplicate := true
	if publicRequest.Deduplicate != nil {
		deduplicate = *publicRequest.Deduplicate
	}
	searchRequest := &models.SearchRequest{
		Query: publicRequest.Query, Domains: append([]string(nil), publicRequest.Domains...),
		Deduplicate: &deduplicate, Limit: publicRequest.Limit, Timeout: timeout,
		Ranking: models.SearchRankingProvider,
	}
	if searchRequest.Limit == 0 {
		searchRequest.Limit = models.DefaultSearchLimit
	}
	response, err := server.search.Search(ctx, searchRequest)
	if err != nil {
		status, code, message := mapSearchError(err)
		return nil, status, code, message
	}
	if err := ctx.Err(); err != nil {
		return nil, http.StatusGatewayTimeout, "SEARCH_TIMEOUT", "search timed out"
	}
	if response == nil || !response.Success || response.Error != nil {
		return nil, http.StatusInternalServerError, "INTERNAL_ERROR", "search failed"
	}
	publicResponse := &publicSearchResponse{
		Success: true, Query: response.Query, Results: make([]publicSearchResult, 0, len(response.Results)),
		Deduplicated: response.Deduplicated, Partial: response.Partial, Timing: response.Timing,
		observation: response.Observation,
	}
	for _, result := range response.Results {
		if result.FetchedAt == nil || result.FetchedAt.IsZero() {
			return nil, http.StatusInternalServerError, "INTERNAL_ERROR", "search result freshness is unavailable"
		}
		publicResponse.Results = append(publicResponse.Results, publicSearchResult{
			Rank: result.Rank, Score: result.Score, Title: result.Title, URL: result.URL,
			Snippet: result.Snippet, FetchedAt: result.FetchedAt,
		})
	}
	return publicResponse, http.StatusOK, "", ""
}

func decodePublicSearchRequest(writer http.ResponseWriter, request *http.Request) (publicSearchRequest, string, string, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, maximumPublicSearchBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var decoded publicSearchRequest
	if err := decoder.Decode(&decoded); err != nil {
		var maximum *http.MaxBytesError
		if errors.As(err, &maximum) {
			return publicSearchRequest{}, "INVALID_INPUT", "request body is too large", false
		}
		return publicSearchRequest{}, "INVALID_INPUT", "invalid search request", false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return publicSearchRequest{}, "INVALID_INPUT", "invalid search request", false
	}
	if decoded.Freshness != nil {
		return publicSearchRequest{}, "UNSUPPORTED_OPTION", "freshness is not supported by the curated index", false
	}
	if decoded.Ranking != "" && decoded.Ranking != "default" && decoded.Ranking != string(models.SearchRankingProvider) {
		return publicSearchRequest{}, "UNSUPPORTED_OPTION", "only provider ranking is available", false
	}
	if decoded.Limit < 0 || decoded.Limit > maximumPublicLimit {
		return publicSearchRequest{}, "INVALID_INPUT", "limit must be between 1 and 10", false
	}
	if decoded.Timeout < 0 || decoded.Timeout > maximumPublicTimeout {
		return publicSearchRequest{}, "INVALID_INPUT", "timeout must be between 1 and 30 seconds", false
	}
	if len(decoded.Domains) > models.MaxSearchDomains {
		return publicSearchRequest{}, "INVALID_INPUT", "too many domains", false
	}
	return decoded, "", "", true
}

func (server *Server) requireAPIKey(writer http.ResponseWriter, request *http.Request) (accounts.AuthenticatedKey, bool) {
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	if len(authorization) < 8 || !strings.EqualFold(authorization[:7], "Bearer ") {
		writeSearchError(writer, http.StatusUnauthorized, "UNAUTHORIZED", "a valid Annolux API key is required")
		return accounts.AuthenticatedKey{}, false
	}
	plaintext := strings.TrimSpace(authorization[7:])
	if strings.ContainsAny(plaintext, " \t\r\n,") {
		writeSearchError(writer, http.StatusUnauthorized, "UNAUTHORIZED", "a valid Annolux API key is required")
		return accounts.AuthenticatedKey{}, false
	}
	authenticated, err := server.accounts.AuthenticateAPIKey(request.Context(), plaintext)
	if errors.Is(err, accounts.ErrSuspended) {
		writeSearchError(writer, http.StatusForbidden, "ACCOUNT_SUSPENDED", "account access is suspended")
		return accounts.AuthenticatedKey{}, false
	}
	if err != nil {
		writeSearchError(writer, http.StatusUnauthorized, "UNAUTHORIZED", "a valid Annolux API key is required")
		return accounts.AuthenticatedKey{}, false
	}
	return authenticated, true
}

func writeSearchError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, searchErrorEnvelope{Success: false, Results: []publicSearchResult{}, Error: errorDetail{Code: code, Message: message}})
}

func mapSearchError(err error) (int, string, string) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, "SEARCH_TIMEOUT", "search timed out"
	}
	var typed *models.ServiceError
	if errors.As(err, &typed) {
		switch typed.Code {
		case models.ErrCodeInvalidInput:
			if strings.Contains(strings.ToLower(typed.Message), "freshness") {
				return http.StatusBadRequest, "UNSUPPORTED_OPTION", "freshness is not supported by the curated index"
			}
			return http.StatusBadRequest, "INVALID_INPUT", "invalid search request"
		case models.ErrCodeRateLimited:
			return http.StatusTooManyRequests, "RATE_LIMITED", "rate limit exceeded"
		case models.ErrCodeSearchFailed:
			return http.StatusBadGateway, "SEARCH_FAILED", "search failed"
		case models.ErrCodeSearchUnavailable:
			return http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "search is unavailable"
		case models.ErrCodeTimeout:
			return http.StatusGatewayTimeout, "SEARCH_TIMEOUT", "search timed out"
		}
	}
	return http.StatusInternalServerError, "INTERNAL_ERROR", "search failed"
}
