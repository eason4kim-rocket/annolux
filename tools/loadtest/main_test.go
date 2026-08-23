package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoadProfileReportsAndPasses(t *testing.T) {
	t.Setenv("ANNOLUX_API_KEY", "ann_live_test")
	t.Setenv("ANNOLUX_LOADTEST_API_KEYS", "")
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer ann_live_test" {
			t.Errorf("authorization was not set")
		}
		calls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"success":true,"results":[]}`))
	}))
	t.Cleanup(server.Close)
	var output bytes.Buffer
	err := run(context.Background(), []string{
		"-api-url", server.URL, "-requests", "8", "-concurrency", "4", "-timeout", "1s", "-confirm-credit-spend",
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 8 || !strings.Contains(output.String(), `"verdict": "pass"`) {
		t.Fatalf("calls=%d output=%s", calls.Load(), output.String())
	}
}

func TestLoadProfileRequiresExplicitSpendConfirmation(t *testing.T) {
	err := validate(options{APIURL: "https://api.annolux.com", APIKeys: []string{"ann_live_test"}, Query: "rust ownership", Requests: 1, Concurrency: 1, Timeout: time.Second, MaximumP95: time.Second, Maximum5xx: .01, Minimum2xx: .99})
	if err == nil || !strings.Contains(err.Error(), "confirm-credit-spend") {
		t.Fatalf("error = %v", err)
	}
}

func TestPercentile(t *testing.T) {
	values := []time.Duration{5 * time.Millisecond, time.Millisecond, 3 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}
	if got := percentile(values, .95); got != 4*time.Millisecond {
		t.Fatalf("p95 = %s", got)
	}
}
