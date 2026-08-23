// Command loadtest measures the bounded, credit-charging public Search route.
// It never prints API keys and refuses to run without explicit credit-spend
// acknowledgement.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type options struct {
	APIURL      string
	APIKeys     []string
	Query       string
	Requests    int
	Concurrency int
	Timeout     time.Duration
	Confirm     bool
	MaximumP95  time.Duration
	Maximum5xx  float64
	Minimum2xx  float64
}

type result struct {
	Status  int
	Latency time.Duration
	Err     error
}

type report struct {
	Requests          int            `json:"requests"`
	Concurrency       int            `json:"concurrency"`
	KeyCount          int            `json:"key_count"`
	Successes         int            `json:"successes_2xx"`
	TransportErrors   int            `json:"transport_errors"`
	StatusCounts      map[string]int `json:"status_counts"`
	ElapsedMS         int64          `json:"elapsed_ms"`
	RequestsPerSecond float64        `json:"requests_per_second"`
	P50MS             int64          `json:"p50_ms"`
	P95MS             int64          `json:"p95_ms"`
	P99MS             int64          `json:"p99_ms"`
	FiveXXRate        float64        `json:"five_xx_rate"`
	SuccessRate       float64        `json:"success_rate"`
	Verdict           string         `json:"verdict"`
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "loadtest: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("loadtest", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configuration := options{APIKeys: loadAPIKeys()}
	flags.StringVar(&configuration.APIURL, "api-url", "", "API base URL")
	flags.StringVar(&configuration.Query, "query", "rust ownership", "fixed launch-profile query")
	flags.IntVar(&configuration.Requests, "requests", 100, "total requests (1..10000)")
	flags.IntVar(&configuration.Concurrency, "concurrency", 12, "workers (1..64)")
	flags.DurationVar(&configuration.Timeout, "timeout", 5*time.Second, "per-request timeout")
	flags.BoolVar(&configuration.Confirm, "confirm-credit-spend", false, "acknowledge that successful requests spend credits")
	flags.DurationVar(&configuration.MaximumP95, "max-p95", time.Second, "release p95 gate")
	flags.Float64Var(&configuration.Maximum5xx, "max-5xx-rate", 0.01, "release 5xx-rate gate")
	flags.Float64Var(&configuration.Minimum2xx, "min-success-rate", 0.99, "minimum 2xx response rate")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return errors.New("invalid invocation")
	}
	if err := validate(configuration); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"query": configuration.Query, "limit": 10, "timeout": int(configuration.Timeout.Seconds()), "deduplicate": true})
	client := &http.Client{Timeout: configuration.Timeout + time.Second}
	started := time.Now()
	jobs := make(chan int)
	results := make(chan result, configuration.Requests)
	var workers sync.WaitGroup
	for worker := 0; worker < configuration.Concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for sequence := range jobs {
				requestContext, cancel := context.WithTimeout(ctx, configuration.Timeout+time.Second)
				request, err := http.NewRequestWithContext(requestContext, http.MethodPost, strings.TrimRight(configuration.APIURL, "/")+"/api/v1/search", bytes.NewReader(body))
				if err == nil {
					request.Header.Set("Authorization", "Bearer "+configuration.APIKeys[sequence%len(configuration.APIKeys)])
					request.Header.Set("Content-Type", "application/json")
				}
				requestStarted := time.Now()
				if err != nil {
					results <- result{Latency: time.Since(requestStarted), Err: err}
					cancel()
					continue
				}
				response, err := client.Do(request)
				latency := time.Since(requestStarted)
				if response != nil {
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
					_ = response.Body.Close()
				}
				status := 0
				if response != nil {
					status = response.StatusCode
				}
				results <- result{Status: status, Latency: latency, Err: err}
				cancel()
			}
		}()
	}
	go func() {
		defer close(jobs)
		for sequence := 0; sequence < configuration.Requests; sequence++ {
			select {
			case jobs <- sequence:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()

	latencies := make([]time.Duration, 0, configuration.Requests)
	statusCounts := make(map[string]int)
	var successes, transportErrors, serverErrors atomic.Int64
	for observed := range results {
		latencies = append(latencies, observed.Latency)
		if observed.Err != nil {
			transportErrors.Add(1)
			statusCounts["transport_error"]++
			continue
		}
		statusCounts[fmt.Sprintf("%d", observed.Status)]++
		if observed.Status >= 200 && observed.Status < 300 {
			successes.Add(1)
		}
		if observed.Status >= 500 {
			serverErrors.Add(1)
		}
	}
	elapsed := time.Since(started)
	measured := len(latencies)
	fiveXXRate := 0.0
	successRate := 0.0
	if measured > 0 {
		fiveXXRate = float64(serverErrors.Load()) / float64(measured)
		successRate = float64(successes.Load()) / float64(measured)
	}
	verdict := "pass"
	if measured != configuration.Requests || transportErrors.Load() > 0 || percentile(latencies, 0.95) > configuration.MaximumP95 || fiveXXRate >= configuration.Maximum5xx || successRate < configuration.Minimum2xx {
		verdict = "fail"
	}
	summary := report{
		Requests: measured, Concurrency: configuration.Concurrency, KeyCount: len(configuration.APIKeys), Successes: int(successes.Load()), TransportErrors: int(transportErrors.Load()),
		StatusCounts: statusCounts, ElapsedMS: elapsed.Milliseconds(), P50MS: percentile(latencies, 0.50).Milliseconds(),
		P95MS: percentile(latencies, 0.95).Milliseconds(), P99MS: percentile(latencies, 0.99).Milliseconds(), FiveXXRate: fiveXXRate, SuccessRate: successRate, Verdict: verdict,
	}
	if elapsed > 0 {
		summary.RequestsPerSecond = float64(measured) / elapsed.Seconds()
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		return err
	}
	if verdict != "pass" {
		return errors.New("launch load profile failed")
	}
	return nil
}

func validate(configuration options) error {
	if !configuration.Confirm {
		return errors.New("-confirm-credit-spend is required")
	}
	if len(configuration.APIKeys) == 0 || len(configuration.APIKeys) > 64 {
		return errors.New("ANNOLUX_API_KEY or ANNOLUX_LOADTEST_API_KEYS is required")
	}
	for _, key := range configuration.APIKeys {
		if !strings.HasPrefix(key, "ann_live_") || strings.ContainsAny(key, " \t\r\n,") {
			return errors.New("load-test API keys must use the Annolux live-key format")
		}
	}
	endpoint, err := url.Parse(configuration.APIURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return errors.New("-api-url must be an absolute URL without credentials, query, or fragment")
	}
	if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && (endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "localhost")) {
		return errors.New("-api-url must use HTTPS or loopback HTTP")
	}
	if configuration.Requests < 1 || configuration.Requests > 10_000 || configuration.Concurrency < 1 || configuration.Concurrency > 64 || configuration.Concurrency > configuration.Requests {
		return errors.New("requests/concurrency are outside safe bounds")
	}
	if len(strings.Fields(configuration.Query)) == 0 || len(strings.Fields(configuration.Query)) > 50 || configuration.Timeout < time.Second || configuration.Timeout > 30*time.Second || configuration.MaximumP95 <= 0 || configuration.Maximum5xx <= 0 || configuration.Maximum5xx > 1 || configuration.Minimum2xx <= 0 || configuration.Minimum2xx > 1 {
		return errors.New("query or gate values are outside the public Search contract")
	}
	return nil
}

func loadAPIKeys() []string {
	raw := strings.TrimSpace(os.Getenv("ANNOLUX_LOADTEST_API_KEYS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("ANNOLUX_API_KEY"))
	}
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(value rune) bool {
		return value == ',' || value == '\n' || value == '\r' || value == '\t' || value == ' '
	})
	keys := make([]string, 0, len(fields))
	for _, key := range fields {
		if key = strings.TrimSpace(key); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func percentile(values []time.Duration, quantile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	copied := append([]time.Duration(nil), values...)
	sort.Slice(copied, func(left, right int) bool { return copied[left] < copied[right] })
	index := int(float64(len(copied)-1) * quantile)
	return copied[index]
}
