package models

import "time"

const (
	DefaultSearchLimit = 10
	MaxSearchLimit     = 10
	MaxSearchDomains   = 20

	MaxSearchQueryRunes    = 400
	MaxSearchQueryWords    = 50
	MaxSearchDomainBytes   = 253
	MaxSearchRequestBytes  = 64 << 10
	MaxSearchResponseBytes = 32 << 20

	DefaultSearchTimeoutSeconds = 30
	MaxSearchTimeoutSeconds     = 30

	ErrCodeSearchUnavailable = "SEARCH_UNAVAILABLE"
	ErrCodeSearchFailed      = "SEARCH_FAILED"
)

// SearchRankingMode is intentionally limited to the curated index's native
// order. The empty value and "provider" are equivalent.
type SearchRankingMode string

const SearchRankingProvider SearchRankingMode = "provider"

// SearchRequest is the complete internal contract for the commercial Search
// surface. Freshness is retained only so non-HTTP callers also fail closed
// rather than silently ignoring it.
type SearchRequest struct {
	Query       string            `json:"query"`
	Domains     []string          `json:"domains,omitempty"`
	Deduplicate *bool             `json:"deduplicate,omitempty"`
	Limit       int               `json:"limit,omitempty"`
	Timeout     int               `json:"timeout,omitempty"`
	Ranking     SearchRankingMode `json:"ranking,omitempty"`
	Freshness   string            `json:"freshness,omitempty"`
}

// Defaults applies the stable commercial defaults without validating input.
func (request *SearchRequest) Defaults() {
	if request == nil {
		return
	}
	if request.Limit == 0 {
		request.Limit = DefaultSearchLimit
	}
	if request.Timeout == 0 {
		request.Timeout = DefaultSearchTimeoutSeconds
	}
	if request.Deduplicate == nil {
		value := true
		request.Deduplicate = &value
	}
}

// SearchResult is one result from the curated local index. FetchedAt is
// mandatory at the public boundary so callers can judge index staleness.
type SearchResult struct {
	Rank      int        `json:"rank"`
	Score     *float64   `json:"score,omitempty"`
	Title     string     `json:"title"`
	URL       string     `json:"url"`
	Snippet   string     `json:"snippet,omitempty"`
	FetchedAt *time.Time `json:"fetched_at"`
}

type SearchTimingInfo struct {
	TotalMs    int64 `json:"total_ms"`
	ProviderMs int64 `json:"provider_ms"`
}

type SearchResponse struct {
	Success      bool             `json:"success"`
	Query        string           `json:"query"`
	Results      []SearchResult   `json:"results"`
	Deduplicated int              `json:"deduplicated"`
	Partial      bool             `json:"partial"`
	Timing       SearchTimingInfo `json:"timing"`
	Error        *ErrorDetail     `json:"error,omitempty"`

	// Observation is in-process telemetry for the query log. It is excluded
	// from serialization so no raw ranking feature can reach a customer
	// response by accident.
	Observation *SearchObservation `json:"-"`
}

// SearchObservation carries the raw boundary features a lexical-confidence
// formula may later be computed from. It deliberately stores components, not
// a combined confidence: the formula will change, recorded features must not.
type SearchObservation struct {
	// QueryLang is the detected query language ("en" or "zh").
	QueryLang string
	// CandidateCount is how many candidates the index returned before any
	// domain filtering, deduplication, or truncation.
	CandidateCount int
	// ScoreGap is the raw score difference between the first two index
	// candidates; nil when fewer than two existed. Raw scores are per
	// tokenizer and comparable only within one language table.
	ScoreGap *float64
	// DomainFiltered counts candidates removed by URL normalization, exact
	// URL deduplication, and the request's domain scope.
	DomainFiltered int
	// NearDupRemoved counts candidates collapsed as near-duplicates.
	NearDupRemoved int
	// OtherLangHits counts returned results whose language differs from the
	// detected query language — the observable trace of cross-language
	// fallback.
	OtherLangHits int
	// Fused reports whether the discovery lane participated; Tier1Hits is
	// how many discovery candidates it contributed before fusion.
	Fused     bool
	Tier1Hits int
	// Served lists the returned documents with their raw scores in response
	// order.
	Served []ServedDocument
}

// ServedDocument is one returned result with its raw index score.
type ServedDocument struct {
	URL   string
	Lang  string
	Score float64
}
