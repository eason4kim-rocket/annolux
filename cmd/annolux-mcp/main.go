package main

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
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/eason4kim-rocket/annolux/models"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const maximumMCPResponseBytes = models.MaxSearchResponseBytes

var version = "dev"

type searchPayload struct {
	Query       string   `json:"query"`
	Domains     []string `json:"domains,omitempty"`
	Deduplicate *bool    `json:"deduplicate,omitempty"`
	Limit       int      `json:"limit,omitempty"`
	Timeout     int      `json:"timeout,omitempty"`
	Ranking     string   `json:"ranking,omitempty"`
}

func main() {
	apiURL := strings.TrimSpace(os.Getenv("ANNOLUX_API_URL"))
	if apiURL == "" {
		apiURL = "https://api.annolux.com"
	}
	normalizedURL, err := normalizeAPIURL(apiURL)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "ANNOLUX_API_URL is invalid")
		os.Exit(1)
	}
	apiKey := strings.TrimSpace(os.Getenv("ANNOLUX_API_KEY"))
	if !strings.HasPrefix(apiKey, "ann_live_") {
		_, _ = fmt.Fprintln(os.Stderr, "ANNOLUX_API_KEY is required")
		os.Exit(1)
	}
	mcpServer := server.NewMCPServer("annolux", version, server.WithToolCapabilities(false))
	mcpServer.AddTool(newSearchWebTool(), handleSearchWeb(newHTTPClient(), normalizedURL, apiKey))
	if err := server.ServeStdio(mcpServer); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "annolux-mcp stopped")
		os.Exit(1)
	}
}

func newSearchWebTool() mcp.Tool {
	tool := mcp.NewTool("search_web",
		mcp.WithDescription("Search Annolux's curated English and Chinese index. One successful call uses one credit."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Natural-language query containing one to fifty words"), mcp.MaxLength(models.MaxSearchQueryRunes)),
		mcp.WithNumber("limit", mcp.Description("Maximum results (default 10, maximum 10)"), mcp.Min(1), mcp.Max(10), mcp.DefaultNumber(10)),
		mcp.WithArray("domains", mcp.Description("Optional registrable-domain filters without schemes or paths"), mcp.MaxItems(models.MaxSearchDomains), mcp.WithStringItems(mcp.MaxLength(models.MaxSearchDomainBytes))),
		mcp.WithBoolean("deduplicate", mcp.Description("Collapse duplicate and near-duplicate results"), mcp.DefaultBool(true)),
		mcp.WithNumber("timeout", mcp.Description("End-to-end timeout in seconds (default and maximum 30)"), mcp.Min(1), mcp.Max(30), mcp.DefaultNumber(30)),
		mcp.WithString("ranking", mcp.Description("Ranking mode: default or provider"), mcp.DefaultString("default")),
		mcp.WithSchemaAdditionalProperties(false),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	for _, name := range []string{"limit", "timeout"} {
		if property, ok := tool.InputSchema.Properties[name].(map[string]any); ok {
			property["type"] = "integer"
		}
	}
	if property, ok := tool.InputSchema.Properties["ranking"].(map[string]any); ok {
		property["enum"] = []string{"default", "provider"}
	}
	return tool
}

func handleSearchWeb(client *http.Client, apiURL, apiKey string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		payload, err := decodeArguments(request.GetArguments())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if ctx == nil {
			return mcp.NewToolResultError("search request context is required"), nil
		}
		taskContext, cancel := context.WithTimeout(ctx, time.Duration(payload.Timeout)*time.Second)
		defer cancel()
		encoded, err := json.Marshal(payload)
		if err != nil {
			return mcp.NewToolResultError("search request could not be encoded"), nil
		}
		httpRequest, err := http.NewRequestWithContext(taskContext, http.MethodPost, apiURL+"/api/v1/search", bytes.NewReader(encoded))
		if err != nil {
			return mcp.NewToolResultError("search request could not be created"), nil
		}
		httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
		httpRequest.Header.Set("Content-Type", "application/json")
		httpRequest.Header.Set("User-Agent", "annolux-mcp/"+version)
		response, err := client.Do(httpRequest)
		if err != nil {
			if taskContext.Err() != nil {
				return mcp.NewToolResultError("search timed out"), nil
			}
			return mcp.NewToolResultError("search request failed"), nil
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, maximumMCPResponseBytes+1))
		if err != nil || len(body) > maximumMCPResponseBytes {
			return mcp.NewToolResultError("search response exceeded its limit"), nil
		}
		if response.StatusCode != http.StatusOK {
			return mcp.NewToolResultError(formatAPIError(response.StatusCode, body)), nil
		}
		var searchResponse map[string]any
		decoder := json.NewDecoder(bytes.NewReader(body))
		if err := decoder.Decode(&searchResponse); err != nil || searchResponse["success"] != true {
			return mcp.NewToolResultError("search returned an invalid response"), nil
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return mcp.NewToolResultError("search returned an invalid response"), nil
		}
		pretty, err := json.MarshalIndent(searchResponse, "", "  ")
		if err != nil {
			return mcp.NewToolResultError("search response could not be formatted"), nil
		}
		return mcp.NewToolResultStructured(searchResponse, string(pretty)), nil
	}
}

func decodeArguments(arguments map[string]any) (searchPayload, error) {
	allowed := map[string]bool{"query": true, "limit": true, "domains": true, "deduplicate": true, "timeout": true, "ranking": true}
	var unknown []string
	for name := range arguments {
		if !allowed[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return searchPayload{}, fmt.Errorf("unsupported search_web argument %q", unknown[0])
	}
	query, ok := arguments["query"].(string)
	if !ok || !validQuery(query) {
		return searchPayload{}, errors.New("query is required and must contain one to fifty words")
	}
	limit, err := optionalInteger(arguments, "limit", 10, 1, 10)
	if err != nil {
		return searchPayload{}, err
	}
	timeout, err := optionalInteger(arguments, "timeout", 30, 1, 30)
	if err != nil {
		return searchPayload{}, err
	}
	deduplicate := true
	if value, present := arguments["deduplicate"]; present {
		var ok bool
		deduplicate, ok = value.(bool)
		if !ok {
			return searchPayload{}, errors.New("deduplicate must be a boolean")
		}
	}
	domains, err := optionalDomains(arguments["domains"])
	if err != nil {
		return searchPayload{}, err
	}
	ranking := "default"
	if value, present := arguments["ranking"]; present {
		var ok bool
		ranking, ok = value.(string)
		if !ok || ranking != "default" && ranking != "provider" {
			return searchPayload{}, errors.New("ranking must be default or provider")
		}
	}
	return searchPayload{Query: query, Limit: limit, Domains: domains, Deduplicate: &deduplicate, Timeout: timeout, Ranking: ranking}, nil
}

func validQuery(query string) bool {
	return query == strings.TrimSpace(query) && query != "" && utf8.ValidString(query) &&
		utf8.RuneCountInString(query) <= models.MaxSearchQueryRunes && len(strings.Fields(query)) <= models.MaxSearchQueryWords &&
		strings.IndexFunc(query, unicode.IsControl) < 0
}

func optionalInteger(arguments map[string]any, name string, fallback, minimum, maximum int) (int, error) {
	value, present := arguments[name]
	if !present {
		return fallback, nil
	}
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < float64(minimum) || number > float64(maximum) {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return int(number), nil
}

func optionalDomains(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	var values []any
	switch typed := raw.(type) {
	case []any:
		values = typed
	case []string:
		values = make([]any, len(typed))
		for index := range typed {
			values[index] = typed[index]
		}
	default:
		return nil, errors.New("domains must be an array of strings")
	}
	if len(values) > models.MaxSearchDomains {
		return nil, errors.New("domains cannot contain more than twenty entries")
	}
	domains := make([]string, len(values))
	for index, value := range values {
		domain, ok := value.(string)
		if !ok || domain == "" || len(domain) > models.MaxSearchDomainBytes || strings.ContainsAny(domain, "/:@?# ") {
			return nil, errors.New("domains must contain only hostnames")
		}
		domains[index] = domain
	}
	return domains, nil
}

func normalizeAPIURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" || parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("invalid API URL")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1")) {
		return "", errors.New("invalid API URL")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       35 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func formatAPIError(status int, body []byte) string {
	var envelope struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil && envelope.Error.Code != "" && envelope.Error.Message != "" {
		return fmt.Sprintf("[%s] %s", envelope.Error.Code, envelope.Error.Message)
	}
	return fmt.Sprintf("search failed (HTTP %d)", status)
}
