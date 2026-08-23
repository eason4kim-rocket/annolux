package indexer

import (
	"errors"
	"fmt"
	"math"
)

const (
	// DefaultMaxDiskUsedPercent stops a production index pass before the
	// filesystem reaches the operator's hard capacity boundary.
	DefaultMaxDiskUsedPercent = 82

	// DefaultMinDiskAvailableBytes keeps enough host capacity for SQLite WAL
	// convergence and the other services sharing the VPS filesystem.
	DefaultMinDiskAvailableBytes uint64 = 25 << 30
)

var (
	// ErrDiskCapacity identifies a deliberate crawl stop caused by either disk
	// threshold. Callers can distinguish it from fetch and indexing failures.
	ErrDiskCapacity = errors.New("indexer: disk capacity guard triggered")

	ErrInvalidDiskCapacity = errors.New("indexer: invalid disk capacity")
)

// CapacityGuard is called before an index pass mutates the frontier, before
// every new lease, and periodically while workers have requests in flight.
// Returning an error stops new work and lets the bounded pending batch flush.
type CapacityGuard func() error

// DiskUsage is a filesystem snapshot. FreeBytes includes reserved blocks;
// AvailableBytes is what the unprivileged indexer process can actually use.
type DiskUsage struct {
	TotalBytes     uint64
	FreeBytes      uint64
	AvailableBytes uint64
}

// UsedPercent follows df-style accounting: reserved blocks count as used and
// the percentage is rounded up so a displayed threshold cannot be crossed by
// a hidden fractional value.
func (usage DiskUsage) UsedPercent() (int, error) {
	if usage.TotalBytes == 0 || usage.FreeBytes > usage.TotalBytes || usage.AvailableBytes > usage.FreeBytes {
		return 0, ErrInvalidDiskCapacity
	}
	usedBytes := usage.TotalBytes - usage.FreeBytes
	denominator := usedBytes + usage.AvailableBytes
	if denominator < usedBytes {
		return 0, ErrInvalidDiskCapacity
	}
	if denominator == 0 {
		return 100, nil
	}
	percent := int(math.Ceil(float64(usedBytes) * 100 / float64(denominator)))
	return min(percent, 100), nil
}

// DiskCapacityPolicy is the fail-closed policy evaluated against every usage
// snapshot. Both boundaries are mandatory so a configuration typo cannot
// silently disable half of the production guardrail.
type DiskCapacityPolicy struct {
	MaxUsedPercent    int
	MinAvailableBytes uint64
}

func (policy DiskCapacityPolicy) Validate() error {
	if policy.MaxUsedPercent < 1 || policy.MaxUsedPercent > 100 || policy.MinAvailableBytes == 0 {
		return ErrInvalidDiskCapacity
	}
	return nil
}

// Check returns a typed capacity error when either threshold is reached.
func (policy DiskCapacityPolicy) Check(usage DiskUsage) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	usedPercent, err := usage.UsedPercent()
	if err != nil {
		return err
	}
	if usedPercent < policy.MaxUsedPercent && usage.AvailableBytes >= policy.MinAvailableBytes {
		return nil
	}
	return &DiskCapacityError{
		UsedPercent:       usedPercent,
		MaxUsedPercent:    policy.MaxUsedPercent,
		AvailableBytes:    usage.AvailableBytes,
		MinAvailableBytes: policy.MinAvailableBytes,
	}
}

// DiskCapacityError records both observed values even when only one boundary
// triggered, so an operator can act without rerunning a diagnostic command.
type DiskCapacityError struct {
	UsedPercent       int
	MaxUsedPercent    int
	AvailableBytes    uint64
	MinAvailableBytes uint64
}

func (err *DiskCapacityError) Error() string {
	if err == nil {
		return ErrDiskCapacity.Error()
	}
	return fmt.Sprintf("%v: used=%d%% limit=%d%% available_bytes=%d minimum_bytes=%d",
		ErrDiskCapacity, err.UsedPercent, err.MaxUsedPercent, err.AvailableBytes, err.MinAvailableBytes)
}

func (err *DiskCapacityError) Unwrap() error { return ErrDiskCapacity }
