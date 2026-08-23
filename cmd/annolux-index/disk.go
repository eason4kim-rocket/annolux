package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"

	"github.com/eason4kim-rocket/annolux/indexer"
)

const gibibyte = uint64(1 << 30)

type diskUsageReader func(string) (indexer.DiskUsage, error)

func newDiskCapacityGuard(outPath string, maxUsedPercent int, minAvailableGiB uint64, read diskUsageReader) (indexer.CapacityGuard, error) {
	if read == nil || minAvailableGiB > math.MaxUint64/gibibyte {
		return nil, indexer.ErrInvalidDiskCapacity
	}
	policy := indexer.DiskCapacityPolicy{
		MaxUsedPercent:    maxUsedPercent,
		MinAvailableBytes: minAvailableGiB * gibibyte,
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return func() error {
		usage, err := read(outPath)
		if err != nil {
			return fmt.Errorf("indexer: inspect disk capacity: %w", err)
		}
		return policy.Check(usage)
	}, nil
}

func filesystemDiskUsage(path string) (indexer.DiskUsage, error) {
	probe, err := nearestExistingPath(path)
	if err != nil {
		return indexer.DiskUsage{}, err
	}
	var stats syscall.Statfs_t
	if err := syscall.Statfs(probe, &stats); err != nil {
		return indexer.DiskUsage{}, fmt.Errorf("statfs %q: %w", probe, err)
	}
	blockSize := uint64(stats.Bsize)
	if blockSize == 0 || uint64(stats.Blocks) > math.MaxUint64/blockSize ||
		uint64(stats.Bfree) > math.MaxUint64/blockSize || uint64(stats.Bavail) > math.MaxUint64/blockSize {
		return indexer.DiskUsage{}, indexer.ErrInvalidDiskCapacity
	}
	return indexer.DiskUsage{
		TotalBytes:     uint64(stats.Blocks) * blockSize,
		FreeBytes:      uint64(stats.Bfree) * blockSize,
		AvailableBytes: uint64(stats.Bavail) * blockSize,
	}, nil
}

func nearestExistingPath(path string) (string, error) {
	probe, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve disk path: %w", err)
	}
	for {
		if _, err := os.Stat(probe); err == nil {
			return probe, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect disk path %q: %w", probe, err)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("inspect disk path %q: no existing ancestor", path)
		}
		probe = parent
	}
}
