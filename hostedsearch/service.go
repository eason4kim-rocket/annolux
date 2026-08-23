// Package hostedsearch implements the deliberately small search surface sold
// by Annolux. It queries only the curated local index and cannot activate Labs
// enrichment, extraction, trust, or external providers.
package hostedsearch

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/eason4kim-rocket/annolux/models"
	"github.com/eason4kim-rocket/annolux/searchfuse"
	"github.com/eason4kim-rocket/annolux/searchindex"
	"github.com/eason4kim-rocket/annolux/simhash"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

const (
	maximumResults        = 10
	maximumCandidatePool  = 20
	maximumTimeoutSeconds = 30
	nearDuplicateDistance = 3
)

// Service owns the hosted Search execution boundary. Store ownership remains
// with the caller so readiness checks and shutdown share one SQLite handle.
type Service struct {
	store *searchindex.Store
	fuser *searchfuse.Fuser
	now   func() time.Time
}

// New constructs a hosted Search service over an open local index with
// fusion disabled: a transparent passthrough to the frozen core query.
func New(store *searchindex.Store) (*Service, error) {
	return NewWithFusion(store, searchfuse.Config{})
}

// NewWithFusion constructs the service with an explicit fusion posture. A
// disabled config behaves bit-for-bit like New.
func NewWithFusion(store *searchindex.Store, fusion searchfuse.Config) (*Service, error) {
	if store == nil {
		return nil, errors.New("hostedsearch: index is required")
	}
	return &Service{store: store, fuser: searchfuse.New(store, fusion), now: time.Now}, nil
}

// Search executes only provider-ordered local-index search. Advanced fields
// fail closed even for trusted in-process callers; the HTTP layer also rejects
// them before credits are reserved.
func (service *Service) Search(ctx context.Context, request *models.SearchRequest) (*models.SearchResponse, error) {
	if ctx == nil {
		return nil, invalidInput("search context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, timeoutError(err)
	}
	if service == nil || service.store == nil || service.now == nil {
		return nil, models.NewServiceError(models.ErrCodeSearchUnavailable, "search is unavailable", nil)
	}
	prepared, err := prepare(request)
	if err != nil {
		return nil, err
	}
	searchContext, cancel := context.WithTimeout(ctx, prepared.timeout)
	defer cancel()
	started := service.now()
	hits, fusion, err := service.fuser.Query(searchContext, prepared.query, maximumCandidatePool)
	providerFinished := service.now()
	if err != nil {
		if searchContext.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, timeoutError(err)
		}
		return nil, models.NewServiceError(models.ErrCodeSearchFailed, "search failed", err)
	}
	if err := searchContext.Err(); err != nil {
		return nil, timeoutError(err)
	}
	candidates := normalizeHits(hits, prepared.domains)
	domainFiltered := len(hits) - len(candidates)
	deduplicated := domainFiltered
	nearDupRemoved := 0
	if prepared.deduplicate {
		candidates, nearDupRemoved = collapseNearDuplicates(candidates)
		deduplicated += nearDupRemoved
	}
	if len(candidates) > prepared.limit {
		candidates = candidates[:prepared.limit]
	}
	// The observation records what this boundary saw, raw: the ranking
	// internals are digest-frozen and stay uninstrumented, so match-tier
	// detail is recovered offline by replaying logged queries against a
	// pinned code commit rather than measured here.
	observation := &models.SearchObservation{
		QueryLang:      searchindex.DetectLanguage(prepared.query),
		CandidateCount: len(hits),
		DomainFiltered: domainFiltered,
		NearDupRemoved: nearDupRemoved,
		Fused:          fusion.Fused,
		Tier1Hits:      fusion.Tier1Hits,
		Served:         make([]models.ServedDocument, 0, len(candidates)),
	}
	if len(hits) >= 2 {
		gap := hits[0].Score - hits[1].Score
		observation.ScoreGap = &gap
	}
	results := make([]models.SearchResult, len(candidates))
	for index, candidate := range candidates {
		fetchedAt := time.Unix(candidate.hit.FetchedAt, 0).UTC()
		results[index] = models.SearchResult{
			Rank: index + 1, Title: candidate.hit.Title, URL: candidate.url,
			Snippet: candidate.hit.Snippet, FetchedAt: &fetchedAt,
		}
		observation.Served = append(observation.Served, models.ServedDocument{
			URL: candidate.url, Lang: candidate.hit.Lang, Score: candidate.hit.Score,
		})
		if candidate.hit.Lang != observation.QueryLang {
			observation.OtherLangHits++
		}
	}
	finished := service.now()
	return &models.SearchResponse{
		Success: true, Query: prepared.query, Results: results,
		Deduplicated: deduplicated, Partial: false,
		Timing: models.SearchTimingInfo{
			ProviderMs: elapsedMilliseconds(started, providerFinished),
			TotalMs:    elapsedMilliseconds(started, finished),
		},
		Observation: observation,
	}, nil
}

type preparedRequest struct {
	query       string
	domains     []string
	deduplicate bool
	limit       int
	timeout     time.Duration
}

func prepare(source *models.SearchRequest) (preparedRequest, error) {
	if source == nil {
		return preparedRequest{}, invalidInput("search request is required")
	}
	if source.Freshness != "" {
		return preparedRequest{}, invalidInput("search freshness is unsupported by this index")
	}
	if source.Ranking != "" && source.Ranking != models.SearchRankingProvider {
		return preparedRequest{}, invalidInput("only provider ranking is available")
	}
	query := strings.Join(strings.Fields(source.Query), " ")
	if query == "" || !utf8.ValidString(query) || utf8.RuneCountInString(query) > models.MaxSearchQueryRunes ||
		len(strings.Fields(query)) > models.MaxSearchQueryWords || strings.IndexFunc(query, unicode.IsControl) >= 0 {
		return preparedRequest{}, invalidInput("search query is invalid")
	}
	limit := source.Limit
	if limit == 0 {
		limit = models.DefaultSearchLimit
	}
	if limit < 1 || limit > maximumResults {
		return preparedRequest{}, invalidInput("search limit must be between 1 and 10")
	}
	timeoutSeconds := source.Timeout
	if timeoutSeconds == 0 {
		timeoutSeconds = maximumTimeoutSeconds
	}
	if timeoutSeconds < 1 || timeoutSeconds > maximumTimeoutSeconds {
		return preparedRequest{}, invalidInput("search timeout must be between 1 and 30 seconds")
	}
	if len(source.Domains) > models.MaxSearchDomains {
		return preparedRequest{}, invalidInput("search domains cannot contain more than 20 entries")
	}
	domains := make([]string, 0, len(source.Domains))
	seenDomains := make(map[string]struct{}, len(source.Domains))
	for _, raw := range source.Domains {
		domain, err := normalizeDomain(raw)
		if err != nil {
			return preparedRequest{}, invalidInput("search domain is invalid")
		}
		if _, exists := seenDomains[domain]; !exists {
			seenDomains[domain] = struct{}{}
			domains = append(domains, domain)
		}
	}
	deduplicate := true
	if source.Deduplicate != nil {
		deduplicate = *source.Deduplicate
	}
	return preparedRequest{
		query: query, domains: domains, deduplicate: deduplicate, limit: limit,
		timeout: time.Duration(timeoutSeconds) * time.Second,
	}, nil
}

type candidate struct {
	hit  searchindex.Hit
	url  string
	host string
}

func normalizeHits(hits []searchindex.Hit, domains []string) []candidate {
	byURL := make(map[string]candidate, len(hits))
	order := make(map[string]int, len(hits))
	for index, hit := range hits {
		canonicalURL, hostname, ok := normalizeResultURL(hit.URL)
		if !ok || !matchesDomain(hostname, domains) {
			continue
		}
		if _, exists := byURL[canonicalURL]; exists {
			continue
		}
		order[canonicalURL] = index
		byURL[canonicalURL] = candidate{hit: hit, url: canonicalURL, host: hostname}
	}
	result := make([]candidate, 0, len(byURL))
	for _, item := range byURL {
		result = append(result, item)
	}
	sort.Slice(result, func(left, right int) bool { return order[result[left].url] < order[result[right].url] })
	return result
}

func collapseNearDuplicates(source []candidate) ([]candidate, int) {
	if len(source) < 2 {
		return source, 0
	}
	fingerprints := make([]uint64, len(source))
	parents := make([]int, len(source))
	for index := range source {
		fingerprints[index] = simhash.Fingerprint(strings.ToLower(strings.TrimSpace(source[index].hit.Title + " " + source[index].hit.Snippet)))
		parents[index] = index
	}
	var find func(int) int
	find = func(index int) int {
		if parents[index] != index {
			parents[index] = find(parents[index])
		}
		return parents[index]
	}
	for left := range source {
		if fingerprints[left] == 0 {
			continue
		}
		for right := left + 1; right < len(source); right++ {
			if fingerprints[right] == 0 || simhash.Distance(fingerprints[left], fingerprints[right]) > nearDuplicateDistance {
				continue
			}
			leftRoot, rightRoot := find(left), find(right)
			if leftRoot != rightRoot {
				parents[rightRoot] = leftRoot
			}
		}
	}
	seen := make(map[int]struct{}, len(source))
	result := make([]candidate, 0, len(source))
	for index, item := range source {
		root := find(index)
		if _, duplicate := seen[root]; duplicate {
			continue
		}
		seen[root] = struct{}{}
		result = append(result, item)
	}
	return result, len(source) - len(result)
}

func normalizeDomain(raw string) (string, error) {
	if raw != strings.TrimSpace(raw) || raw == "" || len(raw) > models.MaxSearchDomainBytes || strings.ContainsAny(raw, "/:@?#*_") {
		return "", errors.New("invalid domain")
	}
	raw = strings.TrimSuffix(raw, ".")
	ascii, err := idna.Lookup.ToASCII(raw)
	if err != nil || ascii == "" || len(ascii) > models.MaxSearchDomainBytes || net.ParseIP(ascii) != nil || strings.EqualFold(ascii, "localhost") {
		return "", errors.New("invalid domain")
	}
	ascii = strings.ToLower(ascii)
	registrable, err := publicsuffix.EffectiveTLDPlusOne(ascii)
	if err != nil || registrable == "" {
		return "", errors.New("invalid domain")
	}
	return ascii, nil
}

func normalizeResultURL(raw string) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", false
	}
	hostname, err := idna.Lookup.ToASCII(parsed.Hostname())
	if err != nil || hostname == "" {
		return "", "", false
	}
	hostname = strings.ToLower(hostname)
	port := parsed.Port()
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = hostname
	if port != "" && !(parsed.Scheme == "https" && port == "443") && !(parsed.Scheme == "http" && port == "80") {
		parsed.Host += ":" + port
	}
	parsed.Fragment = ""
	return parsed.String(), hostname, true
}

func matchesDomain(host string, domains []string) bool {
	if len(domains) == 0 {
		return true
	}
	for _, domain := range domains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func elapsedMilliseconds(start, end time.Time) int64 {
	if end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

func invalidInput(message string) error {
	return models.NewServiceError(models.ErrCodeInvalidInput, message, nil)
}

func timeoutError(cause error) error {
	return models.NewServiceError(models.ErrCodeTimeout, "search timed out", cause)
}
