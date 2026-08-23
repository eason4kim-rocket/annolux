package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckHealthAcceptsOnlyReadyResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantError  bool
	}{
		{name: "ready", statusCode: http.StatusOK, body: `{"status":"ready","version":"test"}`},
		{name: "degraded", statusCode: http.StatusOK, body: `{"status":"degraded"}`, wantError: true},
		{name: "empty status", statusCode: http.StatusOK, body: `{}`, wantError: true},
		{name: "malformed", statusCode: http.StatusOK, body: `{`, wantError: true},
		{name: "trailing JSON", statusCode: http.StatusOK, body: `{"status":"ready"}{}`, wantError: true},
		{name: "server error", statusCode: http.StatusServiceUnavailable, body: `{"status":"ready"}`, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.statusCode)
				_, _ = io.WriteString(writer, test.body)
			}))
			t.Cleanup(server.Close)
			err := checkHealth(context.Background(), newHealthClient(), server.URL)
			if test.wantError != (err != nil) {
				t.Fatalf("checkHealth() error = %v, want error %v", err, test.wantError)
			}
		})
	}
}

func TestCheckHealthRejectsRedirectAndOversizedBody(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"status":"ready"}`)
	}))
	t.Cleanup(healthy.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, healthy.URL, http.StatusFound)
	}))
	t.Cleanup(redirect.Close)
	if err := checkHealth(context.Background(), newHealthClient(), redirect.URL); err == nil {
		t.Fatal("redirect unexpectedly passed healthcheck")
	}

	oversized := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, strings.Repeat("x", maximumHealthBodyBytes+1))
	}))
	t.Cleanup(oversized.Close)
	if err := checkHealth(context.Background(), newHealthClient(), oversized.URL); err == nil {
		t.Fatal("oversized response unexpectedly passed healthcheck")
	}
}

func TestRunRejectsArgumentsAndInvalidInputs(t *testing.T) {
	if err := run(context.Background(), []string{"unexpected"}); !errors.Is(err, errUsage) {
		t.Fatalf("run() error = %v, want usage error", err)
	}
	if err := checkHealth(nil, newHealthClient(), healthEndpoint); !errors.Is(err, errUsage) {
		t.Fatalf("nil-context checkHealth() error = %v", err)
	}
	if err := checkHealth(context.Background(), nil, healthEndpoint); !errors.Is(err, errUsage) {
		t.Fatalf("nil-client checkHealth() error = %v", err)
	}
}
