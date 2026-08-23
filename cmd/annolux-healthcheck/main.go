// Command annolux-healthcheck is the shell-free Docker health probe for the
// Annolux API container.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	healthEndpoint         = "http://127.0.0.1:8080/api/v1/ready"
	healthRequestTimeout   = 3 * time.Second
	maximumHealthBodyBytes = 16 << 10
)

var errUsage = errors.New("annolux-healthcheck: no arguments are accepted")

type healthResponse struct {
	Status string `json:"status"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), healthRequestTimeout)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	if ctx == nil || len(arguments) != 0 {
		return errUsage
	}
	return checkHealth(ctx, newHealthClient(), healthEndpoint)
}

func newHealthClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableKeepAlives = true
	return &http.Client{
		Transport: transport,
		Timeout:   healthRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("health endpoint redirected")
		},
	}
}

func checkHealth(ctx context.Context, client *http.Client, endpoint string) error {
	if ctx == nil || client == nil || endpoint == "" {
		return errUsage
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("annolux-healthcheck: build request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("annolux-healthcheck: request failed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumHealthBodyBytes+1))
	if err != nil {
		return fmt.Errorf("annolux-healthcheck: read response: %w", err)
	}
	if len(body) > maximumHealthBodyBytes {
		return errors.New("annolux-healthcheck: response body exceeds 16 KiB")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("annolux-healthcheck: unexpected HTTP status %d", response.StatusCode)
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	var health healthResponse
	if err := decoder.Decode(&health); err != nil {
		return fmt.Errorf("annolux-healthcheck: decode response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("annolux-healthcheck: response contains trailing JSON")
		}
		return fmt.Errorf("annolux-healthcheck: decode trailing response: %w", err)
	}
	if health.Status != "ready" {
		return fmt.Errorf("annolux-healthcheck: service status is %q", health.Status)
	}
	return nil
}
