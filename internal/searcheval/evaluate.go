package searcheval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/eason4kim-rocket/annolux/models"
)

const (
	evaluationUserAgent = "annolux-searcheval/1.0"
	maximumAPIKeyBytes  = 16 << 10
)

// IndexIdentity records enough immutable index state to compare two rounds
// without hashing a multi-gigabyte SQLite file.
type IndexIdentity struct {
	Pages           int64 `json:"pages"`
	EnglishPages    int64 `json:"english_pages"`
	ChinesePages    int64 `json:"chinese_pages"`
	LatestFetchedAt int64 `json:"latest_fetched_at"`
	DatabaseBytes   int64 `json:"database_bytes"`
	WALBytes        int64 `json:"wal_bytes"`
}

// RunOptions binds a round to exact code, image, corpus, and index identities.
type RunOptions struct {
	RunID           string
	APIURL          string
	APIKey          string
	CodeCommit      string
	EvaluatorCommit string
	ImageDigest     string
	Index           IndexIdentity
	Split           string
	Timeout         time.Duration
}

// RunReport is the in-memory representation written as a three-record-kind
// JSONL file: run metadata, one row per case, and a final aggregate summary.
type RunReport struct {
	Metadata RunMetadata
	Cases    []CaseResult
	Summary  Summary
}

type RunMetadata struct {
	Type              string        `json:"type"`
	SchemaVersion     int           `json:"schema_version"`
	RunID             string        `json:"run_id"`
	StartedAt         time.Time     `json:"started_at"`
	FinishedAt        time.Time     `json:"finished_at"`
	APIURL            string        `json:"api_url"`
	CodeCommit        string        `json:"code_commit"`
	EvaluatorCommit   string        `json:"evaluator_commit"`
	ImageDigest       string        `json:"image_digest"`
	Index             IndexIdentity `json:"index"`
	CorpusName        string        `json:"corpus_name"`
	CorpusSHA256      string        `json:"corpus_sha256"`
	ManifestSHA256    string        `json:"manifest_sha256"`
	FrozenAt          string        `json:"corpus_frozen_at"`
	Split             string        `json:"split"`
	Limit             int           `json:"limit"`
	RequestTimeoutMS  int64         `json:"request_timeout_ms"`
	RequestIntervalMS int           `json:"request_interval_ms"`
}

type ObservedResult struct {
	Rank  int    `json:"rank"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Grade int    `json:"judged_grade,omitempty"`
}

type CaseResult struct {
	Type                 string           `json:"type"`
	ID                   string           `json:"id"`
	Split                string           `json:"split"`
	Language             string           `json:"language"`
	Category             string           `json:"category"`
	Query                string           `json:"query"`
	Targets              []Target         `json:"targets"`
	Tags                 []string         `json:"tags,omitempty"`
	HTTPStatus           int              `json:"http_status"`
	ClientLatencyMS      int64            `json:"client_latency_ms"`
	ServerLatencyMS      int64            `json:"server_latency_ms,omitempty"`
	ResponseDeduplicated int              `json:"response_deduplicated"`
	DuplicateURLs        int              `json:"duplicate_urls"`
	SourceTwinHits       int              `json:"source_twin_hits"`
	PrimaryTargetRank    int              `json:"primary_target_rank,omitempty"`
	BestTargetRank       int              `json:"best_target_rank,omitempty"`
	BestTargetGrade      int              `json:"best_target_grade,omitempty"`
	ReciprocalRank       float64          `json:"reciprocal_rank"`
	NDCG                 float64          `json:"ndcg"`
	Verdict              string           `json:"verdict"`
	Error                string           `json:"error,omitempty"`
	Results              []ObservedResult `json:"results"`
}

type BucketSummary struct {
	Total      int     `json:"total"`
	Errors     int     `json:"errors"`
	Strong     int     `json:"strong"`
	Acceptable int     `json:"acceptable"`
	Weak       int     `json:"weak"`
	Miss       int     `json:"miss"`
	HitAt1     float64 `json:"hit_at_1"`
	HitAt3     float64 `json:"hit_at_3"`
	HitAt10    float64 `json:"hit_at_10"`
	MRRAt10    float64 `json:"mrr_at_10"`
	MeanNDCG   float64 `json:"mean_ndcg"`
}

type Summary struct {
	Type                   string                   `json:"type"`
	Overall                BucketSummary            `json:"overall"`
	BySplit                map[string]BucketSummary `json:"by_split"`
	ByLanguage             map[string]BucketSummary `json:"by_language"`
	ByCategory             map[string]BucketSummary `json:"by_category"`
	ClientLatencySamples   int                      `json:"client_latency_samples"`
	ClientMedianLatencyMS  int64                    `json:"client_median_latency_ms"`
	ClientP95LatencyMS     int64                    `json:"client_p95_latency_ms"`
	ClientMaximumLatencyMS int64                    `json:"client_maximum_latency_ms"`
	ServerLatencySamples   int                      `json:"server_latency_samples"`
	ServerMedianLatencyMS  int64                    `json:"server_median_latency_ms"`
	ServerP95LatencyMS     int64                    `json:"server_p95_latency_ms"`
	ServerMaximumLatencyMS int64                    `json:"server_maximum_latency_ms"`
	DuplicateURLs          int                      `json:"duplicate_urls"`
	SourceTwinHits         int                      `json:"source_twin_hits"`
	ResponseDeduped        int                      `json:"response_deduplicated"`
}

// Evaluate runs cases sequentially so the benchmark does not become a load
// test. Transport or response failures are recorded per case and do not erase
// the rest of the round.
func Evaluate(ctx context.Context, client *http.Client, corpus Corpus, options RunOptions) (RunReport, error) {
	if ctx == nil || client == nil {
		return RunReport{}, errors.New("evaluation context and client are required")
	}
	if err := ctx.Err(); err != nil {
		return RunReport{}, err
	}
	baseURL, err := validateRunOptions(options)
	if err != nil {
		return RunReport{}, err
	}
	cases, err := corpus.CasesForSplit(options.Split)
	if err != nil {
		return RunReport{}, err
	}
	startedAt := time.Now().UTC()
	results := make([]CaseResult, 0, len(cases))
	for index, testCase := range cases {
		if err := ctx.Err(); err != nil {
			return RunReport{}, err
		}
		results = append(results, evaluateCase(
			ctx, client, baseURL, corpus.Manifest.Protocol, testCase, options.Timeout, options.APIKey,
		))
		if index+1 < len(cases) {
			if err := waitForNextRequest(ctx, time.Duration(corpus.Manifest.Protocol.RequestIntervalMS)*time.Millisecond); err != nil {
				return RunReport{}, err
			}
		}
	}
	finishedAt := time.Now().UTC()
	metadata := RunMetadata{
		Type: "run", SchemaVersion: SchemaVersion, RunID: options.RunID,
		StartedAt: startedAt, FinishedAt: finishedAt, APIURL: baseURL.String(),
		CodeCommit: options.CodeCommit, EvaluatorCommit: options.EvaluatorCommit,
		ImageDigest: options.ImageDigest, Index: options.Index,
		CorpusName: corpus.Manifest.Name, CorpusSHA256: corpus.Digest, ManifestSHA256: corpus.ManifestDigest,
		FrozenAt: corpus.Manifest.FrozenAt,
		Split:    options.Split, Limit: corpus.Manifest.Protocol.Limit, RequestTimeoutMS: options.Timeout.Milliseconds(),
		RequestIntervalMS: corpus.Manifest.Protocol.RequestIntervalMS,
	}
	return RunReport{Metadata: metadata, Cases: results, Summary: summarize(results)}, nil
}

func evaluateCase(
	ctx context.Context,
	client *http.Client,
	baseURL *url.URL,
	protocol Protocol,
	testCase Case,
	timeout time.Duration,
	apiKey string,
) CaseResult {
	record := CaseResult{
		Type: "case", ID: testCase.ID, Split: testCase.Split, Language: testCase.Language,
		Category: testCase.Category, Query: testCase.Query, Targets: testCase.Targets,
		Tags: testCase.Tags, Verdict: "error", Results: []ObservedResult{},
	}
	payload, _ := json.Marshal(struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}{Query: testCase.Query, Limit: protocol.Limit})
	endpoint := baseURL.ResolveReference(&url.URL{Path: "/api/v1/search"})
	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		record.Error = "build_request"
		return record
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", evaluationUserAgent)
	request.Header.Set("Authorization", "Bearer "+apiKey)
	started := time.Now()
	response, err := client.Do(request)
	record.ClientLatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		record.Error = "request_failed"
		return record
	}
	defer response.Body.Close()
	record.HTTPStatus = response.StatusCode
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		record.Error = fmt.Sprintf("http_%d", response.StatusCode)
		return record
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, models.MaxSearchResponseBytes+1))
	if err != nil || len(body) > models.MaxSearchResponseBytes {
		record.Error = "read_response"
		return record
	}
	var searchResponse models.SearchResponse
	if err := json.Unmarshal(body, &searchResponse); err != nil || !searchResponse.Success || searchResponse.Error != nil {
		record.Error = "invalid_response"
		return record
	}
	if searchResponse.Query != testCase.Query || len(searchResponse.Results) > protocol.Limit {
		record.Error = "response_contract"
		return record
	}
	record.ServerLatencyMS = searchResponse.Timing.TotalMs
	record.ResponseDeduplicated = searchResponse.Deduplicated
	gradeByURL := make(map[string]int, len(testCase.Targets))
	idealGrades := make([]int, 0, len(testCase.Targets))
	for _, target := range testCase.Targets {
		key, _ := CanonicalURLKey(target.URL)
		gradeByURL[key] = target.Relevance
		idealGrades = append(idealGrades, target.Relevance)
	}
	seen := make(map[string]struct{}, len(searchResponse.Results))
	observedGrades := make([]int, 0, len(searchResponse.Results))
	for index, result := range searchResponse.Results {
		grade := 0
		key, keyErr := CanonicalURLKey(result.URL)
		if keyErr == nil {
			grade = gradeByURL[key]
			if _, exists := seen[key]; exists {
				record.DuplicateURLs++
			}
			seen[key] = struct{}{}
		}
		if sourceTwinURL(result.URL) {
			record.SourceTwinHits++
		}
		rank := index + 1
		if result.Rank > 0 {
			rank = result.Rank
		}
		record.Results = append(record.Results, ObservedResult{Rank: rank, Title: result.Title, URL: result.URL, Grade: grade})
		observedGrades = append(observedGrades, grade)
		if grade > 0 && record.BestTargetRank == 0 {
			record.BestTargetRank = rank
			record.BestTargetGrade = grade
		}
		if grade >= protocol.StrongGrade && record.PrimaryTargetRank == 0 {
			record.PrimaryTargetRank = rank
		}
	}
	if record.BestTargetRank > 0 && record.BestTargetRank <= protocol.Limit {
		record.ReciprocalRank = 1 / float64(record.BestTargetRank)
	}
	record.NDCG = normalizedDCG(observedGrades, idealGrades, protocol.Limit)
	switch {
	case record.PrimaryTargetRank == 1:
		record.Verdict = "strong"
	case record.BestTargetRank > 0 && record.BestTargetRank <= 3 && record.BestTargetGrade >= protocol.RelevantGrade:
		record.Verdict = "acceptable"
	case record.BestTargetRank > 0 && record.BestTargetRank <= protocol.Limit:
		record.Verdict = "weak"
	default:
		record.Verdict = "miss"
	}
	return record
}

func validateRunOptions(options RunOptions) (*url.URL, error) {
	if !identifierPattern.MatchString(options.RunID) || strings.TrimSpace(options.CodeCommit) == "" ||
		strings.TrimSpace(options.EvaluatorCommit) == "" ||
		strings.TrimSpace(options.ImageDigest) == "" || options.Timeout <= 0 || options.Split == "" {
		return nil, errors.New("run identity is incomplete")
	}
	if !strings.HasPrefix(options.APIKey, "ann_live_") || len(options.APIKey) > maximumAPIKeyBytes || strings.TrimSpace(options.APIKey) != options.APIKey ||
		strings.ContainsAny(options.APIKey, "\r\n") {
		return nil, errors.New("API credential is invalid")
	}
	parsed, err := url.Parse(options.APIURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("API URL is invalid")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	if options.Index.Pages < 1 || options.Index.EnglishPages < 0 || options.Index.ChinesePages < 0 ||
		options.Index.EnglishPages+options.Index.ChinesePages != options.Index.Pages ||
		options.Index.LatestFetchedAt < 1 || options.Index.DatabaseBytes < 1 || options.Index.WALBytes < 0 {
		return nil, errors.New("index identity is invalid")
	}
	return parsed, nil
}

func summarize(results []CaseResult) Summary {
	summary := Summary{
		Type: "summary", BySplit: make(map[string]BucketSummary),
		ByLanguage: make(map[string]BucketSummary), ByCategory: make(map[string]BucketSummary),
	}
	clientLatencies := make([]int64, 0, len(results))
	serverLatencies := make([]int64, 0, len(results))
	for _, result := range results {
		clientLatencies = append(clientLatencies, result.ClientLatencyMS)
		if result.Error == "" {
			serverLatencies = append(serverLatencies, result.ServerLatencyMS)
		}
		summary.DuplicateURLs += result.DuplicateURLs
		summary.SourceTwinHits += result.SourceTwinHits
		summary.ResponseDeduped += result.ResponseDeduplicated
	}
	summary.Overall = summarizeBucket(results)
	for _, dimension := range []struct {
		destination map[string]BucketSummary
		key         func(CaseResult) string
	}{
		{summary.BySplit, func(result CaseResult) string { return result.Split }},
		{summary.ByLanguage, func(result CaseResult) string { return result.Language }},
		{summary.ByCategory, func(result CaseResult) string { return result.Category }},
	} {
		grouped := make(map[string][]CaseResult)
		for _, result := range results {
			key := dimension.key(result)
			grouped[key] = append(grouped[key], result)
		}
		for key, group := range grouped {
			dimension.destination[key] = summarizeBucket(group)
		}
	}
	sort.Slice(clientLatencies, func(left, right int) bool { return clientLatencies[left] < clientLatencies[right] })
	sort.Slice(serverLatencies, func(left, right int) bool { return serverLatencies[left] < serverLatencies[right] })
	summary.ClientLatencySamples = len(clientLatencies)
	summary.ClientMedianLatencyMS = nearestRank(clientLatencies, 0.50)
	summary.ClientP95LatencyMS = nearestRank(clientLatencies, 0.95)
	summary.ClientMaximumLatencyMS = nearestRank(clientLatencies, 1)
	summary.ServerLatencySamples = len(serverLatencies)
	summary.ServerMedianLatencyMS = nearestRank(serverLatencies, 0.50)
	summary.ServerP95LatencyMS = nearestRank(serverLatencies, 0.95)
	summary.ServerMaximumLatencyMS = nearestRank(serverLatencies, 1)
	return summary
}

func summarizeBucket(results []CaseResult) BucketSummary {
	bucket := BucketSummary{Total: len(results)}
	if len(results) == 0 {
		return bucket
	}
	var hits1, hits3, hits10 int
	var reciprocalRank, ndcg float64
	for _, result := range results {
		switch result.Verdict {
		case "strong":
			bucket.Strong++
		case "acceptable":
			bucket.Acceptable++
		case "weak":
			bucket.Weak++
		case "miss":
			bucket.Miss++
		default:
			bucket.Errors++
		}
		if result.BestTargetRank == 1 {
			hits1++
		}
		if result.BestTargetRank > 0 && result.BestTargetRank <= 3 {
			hits3++
		}
		if result.BestTargetRank > 0 && result.BestTargetRank <= 10 {
			hits10++
		}
		reciprocalRank += result.ReciprocalRank
		ndcg += result.NDCG
	}
	denominator := float64(len(results))
	bucket.HitAt1 = float64(hits1) / denominator
	bucket.HitAt3 = float64(hits3) / denominator
	bucket.HitAt10 = float64(hits10) / denominator
	bucket.MRRAt10 = reciprocalRank / denominator
	bucket.MeanNDCG = ndcg / denominator
	return bucket
}

func normalizedDCG(observedGrades, idealGrades []int, limit int) float64 {
	ideal := append([]int(nil), idealGrades...)
	sort.Sort(sort.Reverse(sort.IntSlice(ideal)))
	dcg := discountedGain(observedGrades, limit)
	idcg := discountedGain(ideal, limit)
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func discountedGain(grades []int, limit int) float64 {
	if len(grades) > limit {
		grades = grades[:limit]
	}
	var result float64
	for index, grade := range grades {
		if grade > 0 {
			result += (math.Pow(2, float64(grade)) - 1) / math.Log2(float64(index+2))
		}
	}
	return result
}

func nearestRank(values []int64, quantile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	rank := int(math.Ceil(quantile*float64(len(values)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(values) {
		rank = len(values) - 1
	}
	return values[rank]
}

func waitForNextRequest(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func sourceTwinURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	for _, segment := range strings.Split(parsed.EscapedPath(), "/") {
		if strings.EqualFold(segment, "_sources") {
			return true
		}
	}
	return false
}

// EncodeJSONL emits a complete self-describing round. Results are intended to
// be installed with an exclusive, atomic write by the command wrapper.
func EncodeJSONL(report RunReport) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(report.Metadata); err != nil {
		return nil, err
	}
	for _, result := range report.Cases {
		if err := encoder.Encode(result); err != nil {
			return nil, err
		}
	}
	if err := encoder.Encode(report.Summary); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
