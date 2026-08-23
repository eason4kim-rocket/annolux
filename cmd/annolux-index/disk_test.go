package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/eason4kim-rocket/annolux/indexer"
)

func TestNewDiskCapacityGuardUsesProductionThresholds(t *testing.T) {
	const outPath = "/data/index.db"
	var gotPath string
	guard, err := newDiskCapacityGuard(outPath, indexer.DefaultMaxDiskUsedPercent, 25, func(path string) (indexer.DiskUsage, error) {
		gotPath = path
		return indexer.DiskUsage{
			TotalBytes: 200 << 30, FreeBytes: 36 << 30, AvailableBytes: 36 << 30,
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := guard(); !errors.Is(err, indexer.ErrDiskCapacity) {
		t.Fatalf("guard() error = %v, want capacity stop", err)
	}
	if gotPath != outPath {
		t.Fatalf("usage path = %q, want %q", gotPath, outPath)
	}
}

func TestNewDiskCapacityGuardFailsClosed(t *testing.T) {
	readErr := errors.New("statfs failed")
	guard, err := newDiskCapacityGuard("index.db", 82, 25, func(string) (indexer.DiskUsage, error) {
		return indexer.DiskUsage{}, readErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := guard(); !errors.Is(err, readErr) {
		t.Fatalf("guard() error = %v, want statfs failure", err)
	}
	for _, input := range []struct {
		percent int
		freeGiB uint64
	}{
		{percent: 0, freeGiB: 25},
		{percent: 101, freeGiB: 25},
		{percent: 82, freeGiB: 0},
	} {
		if _, err := newDiskCapacityGuard("index.db", input.percent, input.freeGiB, filesystemDiskUsage); !errors.Is(err, indexer.ErrInvalidDiskCapacity) {
			t.Fatalf("newDiskCapacityGuard(%d, %d) error = %v", input.percent, input.freeGiB, err)
		}
	}
}

func TestFilesystemDiskUsageUsesNearestExistingAncestor(t *testing.T) {
	missingDatabase := filepath.Join(t.TempDir(), "new", "nested", "index.db")
	usage, err := filesystemDiskUsage(missingDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if usage.TotalBytes == 0 || usage.FreeBytes > usage.TotalBytes || usage.AvailableBytes > usage.FreeBytes {
		t.Fatalf("filesystemDiskUsage() = %#v", usage)
	}
}
