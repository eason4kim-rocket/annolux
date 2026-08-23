package main

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

type testRoundTrip func(*http.Request) (*http.Response, error)

func (function testRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestSearchToolExposesOnlyCommercialSearchArguments(t *testing.T) {
	tool := newSearchWebTool()
	if tool.Name != "search_web" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	var names []string
	for name := range tool.InputSchema.Properties {
		names = append(names, name)
	}
	slicesSort(names)
	want := []string{"deduplicate", "domains", "limit", "query", "ranking", "timeout"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("properties = %v, want %v", names, want)
	}
}

func TestSearchHandlerSendsOnlyBoundedPayloadAndKeepsKeyInHeader(t *testing.T) {
	apiKey := "ann_live_test_secret_value_that_must_not_leak"
	client := &http.Client{Transport: testRoundTrip(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		if request.URL.String() != "https://annolux.com/api/v1/search" || request.Header.Get("Authorization") != "Bearer "+apiKey || strings.Contains(string(body), apiKey) {
			t.Fatalf("request url=%s auth=%q body=%s", request.URL, request.Header.Get("Authorization"), body)
		}
		if strings.Contains(string(body), "verify") || strings.Contains(string(body), "freshness") || !strings.Contains(string(body), `"ranking":"default"`) {
			t.Fatalf("advanced field in body: %s", body)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"success":true,"query":"rust ownership","results":[],"deduplicated":0,"dropped_stale":0,"partial":false,"timing":{"total_ms":1,"provider_ms":1}}`))}, nil
	})}
	handler := handleSearchWeb(client, "https://annolux.com", apiKey)
	result, err := handler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"query": "rust ownership", "limit": float64(5), "deduplicate": true, "timeout": float64(10)}}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("handler result = %#v, %v", result, err)
	}
}

func TestDecodeArgumentsRejectsOldCapabilities(t *testing.T) {
	for _, name := range []string{"freshness", "verify", "include_content", "schema", "llm_api_key"} {
		if _, err := decodeArguments(map[string]any{"query": "test", name: true}); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	if _, err := decodeArguments(map[string]any{"query": "test", "ranking": "trust"}); err == nil {
		t.Fatal("accepted trust ranking")
	}
	if _, err := decodeArguments(map[string]any{"query": "test", "limit": float64(11)}); err == nil {
		t.Fatal("accepted limit 11")
	}
}

func TestNormalizeAPIURLRejectsCredentialAndNonTLSRemote(t *testing.T) {
	for _, raw := range []string{"http://annolux.com", "https://user:pass@annolux.com", "https://annolux.com/path", "https://annolux.com?q=1"} {
		if _, err := normalizeAPIURL(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"https://annolux.com", "http://127.0.0.1:8080"} {
		if _, err := normalizeAPIURL(raw); err != nil {
			t.Fatalf("rejected %q: %v", raw, err)
		}
	}
}

func slicesSort(values []string) {
	for index := 1; index < len(values); index++ {
		for cursor := index; cursor > 0 && values[cursor] < values[cursor-1]; cursor-- {
			values[cursor], values[cursor-1] = values[cursor-1], values[cursor]
		}
	}
}
