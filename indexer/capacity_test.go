package indexer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eason4kim-rocket/annolux/searchindex"
)

func TestDiskCapacityPolicyStopsAtEitherProductionBoundary(t *testing.T) {
	const gib = uint64(1 << 30)
	policy := DiskCapacityPolicy{
		MaxUsedPercent:    DefaultMaxDiskUsedPercent,
		MinAvailableBytes: DefaultMinDiskAvailableBytes,
	}
	tests := []struct {
		name     string
		usage    DiskUsage
		wantStop bool
		wantUsed int
	}{
		{
			name:     "safe",
			usage:    DiskUsage{TotalBytes: 200 * gib, FreeBytes: 40 * gib, AvailableBytes: 40 * gib},
			wantUsed: 80,
		},
		{
			name:     "available bytes equal minimum",
			usage:    DiskUsage{TotalBytes: 100 * gib, FreeBytes: 25 * gib, AvailableBytes: 25 * gib},
			wantUsed: 75,
		},
		{
			name:     "used percentage reaches limit",
			usage:    DiskUsage{TotalBytes: 200 * gib, FreeBytes: 36 * gib, AvailableBytes: 36 * gib},
			wantStop: true, wantUsed: 82,
		},
		{
			name:     "available bytes fall below minimum",
			usage:    DiskUsage{TotalBytes: 40 * gib, FreeBytes: 20 * gib, AvailableBytes: 20 * gib},
			wantStop: true, wantUsed: 50,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			used, err := test.usage.UsedPercent()
			if err != nil || used != test.wantUsed {
				t.Fatalf("UsedPercent() = %d, %v, want %d", used, err, test.wantUsed)
			}
			err = policy.Check(test.usage)
			if test.wantStop != errors.Is(err, ErrDiskCapacity) {
				t.Fatalf("Check() error = %v, want stop %v", err, test.wantStop)
			}
		})
	}
}

func TestDiskCapacityPolicyRejectsInvalidInput(t *testing.T) {
	validUsage := DiskUsage{TotalBytes: 100, FreeBytes: 50, AvailableBytes: 50}
	for _, policy := range []DiskCapacityPolicy{
		{},
		{MaxUsedPercent: 101, MinAvailableBytes: 1},
		{MaxUsedPercent: 82},
	} {
		if !errors.Is(policy.Check(validUsage), ErrInvalidDiskCapacity) {
			t.Fatalf("policy %#v did not fail closed", policy)
		}
	}
	for _, usage := range []DiskUsage{
		{},
		{TotalBytes: 100, FreeBytes: 101, AvailableBytes: 1},
		{TotalBytes: 100, FreeBytes: 50, AvailableBytes: 51},
	} {
		if _, err := usage.UsedPercent(); !errors.Is(err, ErrInvalidDiskCapacity) {
			t.Fatalf("usage %#v did not fail closed: %v", usage, err)
		}
	}
}

func TestRunChecksCapacityBeforeFrontierMutation(t *testing.T) {
	store, err := searchindex.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fetcher, err := NewFetcher(FetcherConfig{AllowPrivateNetworks: true})
	if err != nil {
		t.Fatal(err)
	}
	guardErr := &DiskCapacityError{
		UsedPercent: 82, MaxUsedPercent: 82,
		AvailableBytes: 24 << 30, MinAvailableBytes: 25 << 30,
	}
	_, err = Run(context.Background(), store, fetcher, stubDiscoverer{}, []string{"http://127.0.0.1/"}, RunConfig{
		Workers: 1, CapacityGuard: func() error { return guardErr }, AllowPrivate: true,
	})
	if !errors.Is(err, ErrDiskCapacity) {
		t.Fatalf("Run() error = %v, want disk capacity stop", err)
	}
	active, err := store.ActiveFrontier(context.Background())
	if err != nil || active != 0 {
		t.Fatalf("ActiveFrontier() = %d, %v, want no preflight mutation", active, err)
	}
}

func TestRunStopsNewLeasesAndFlushesPendingBatch(t *testing.T) {
	var pageFetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/robots.txt" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		pageFetches.Add(1)
		_, _ = io.WriteString(writer, "<html><title>Guarded page</title><body>"+
			strings.Repeat("bounded indexed content ", 40)+"</body></html>")
	}))
	t.Cleanup(server.Close)

	store, err := searchindex.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fetcher, err := NewFetcher(FetcherConfig{AllowPrivateNetworks: true})
	if err != nil {
		t.Fatal(err)
	}
	guardErr := errors.New("capacity sample crossed threshold")
	var checks atomic.Int32
	stats, err := Run(context.Background(), store, fetcher, stubDiscoverer{}, []string{server.URL + "/"}, RunConfig{
		Workers: 1, BatchSize: 10, MaxPages: 10, AllowPrivate: true,
		CapacityCheckInterval: time.Hour,
		CapacityGuard: func() error {
			if checks.Add(1) >= 3 {
				return guardErr
			}
			return nil
		},
	})
	if !errors.Is(err, guardErr) {
		t.Fatalf("Run() error = %v, want %v", err, guardErr)
	}
	if stats.Indexed != 1 || pageFetches.Load() != 1 {
		t.Fatalf("stats/fetches = %#v/%d, want one flushed in-flight page", stats, pageFetches.Load())
	}
	hits, err := store.Query(context.Background(), "bounded indexed content", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("Query() = %#v, %v, want pending batch flushed", hits, err)
	}
}

func TestRunChecksCapacityWhileWorkerIsInFlight(t *testing.T) {
	pageStarted := make(chan struct{})
	releasePage := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePage) }) }
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/robots.txt" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		close(pageStarted)
		<-releasePage
		_, _ = io.WriteString(writer, "<html><title>In flight</title><body>"+
			strings.Repeat("periodic capacity check ", 40)+"</body></html>")
	}))
	t.Cleanup(server.Close)
	t.Cleanup(release)

	store, err := searchindex.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fetcher, err := NewFetcher(FetcherConfig{AllowPrivateNetworks: true})
	if err != nil {
		t.Fatal(err)
	}
	guardErr := errors.New("periodic capacity stop")
	guardTriggered := make(chan struct{})
	var checks atomic.Int32
	result := make(chan error, 1)
	go func() {
		_, runErr := Run(context.Background(), store, fetcher, stubDiscoverer{}, []string{server.URL + "/"}, RunConfig{
			Workers: 1, BatchSize: 10, MaxPages: 10, AllowPrivate: true,
			CapacityCheckInterval: 10 * time.Millisecond,
			CapacityGuard: func() error {
				if checks.Add(1) < 3 {
					return nil
				}
				select {
				case <-guardTriggered:
				default:
					close(guardTriggered)
				}
				return guardErr
			},
		})
		result <- runErr
	}()
	select {
	case <-pageStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start its page fetch")
	}
	select {
	case <-guardTriggered:
	case <-time.After(5 * time.Second):
		t.Fatal("capacity monitor did not run while the worker was in flight")
	}
	release()
	select {
	case err := <-result:
		if !errors.Is(err, guardErr) {
			t.Fatalf("Run() error = %v, want %v", err, guardErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not finish after the in-flight page completed")
	}
}
