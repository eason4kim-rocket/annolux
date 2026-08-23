package main

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/eason4kim-rocket/annolux/querylog"
)

// runQueryLogCommand serves the two operator verbs over the query log:
// stats for verification, expire for manually enforcing the retention
// promise the API also enforces in-process.
func runQueryLogCommand(arguments []string) error {
	if len(arguments) < 2 {
		return usageError()
	}
	path := arguments[1]
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		return errors.New("query log must be an existing regular file")
	}
	store, err := querylog.Open(path)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	switch arguments[0] {
	case "querylog-stats":
		if len(arguments) != 2 {
			return usageError()
		}
		stats, err := store.Stats(ctx)
		if err != nil {
			return err
		}
		return writeJSON(stats)
	case "querylog-expire":
		days := 30
		if len(arguments) == 3 {
			parsed, err := strconv.Atoi(arguments[2])
			if err != nil || parsed < 1 || parsed > 365 {
				return errors.New("retention days must be an integer between 1 and 365")
			}
			days = parsed
		} else if len(arguments) != 2 {
			return usageError()
		}
		redacted, err := store.ExpireQueryText(ctx, time.Now().Add(-time.Duration(days)*24*time.Hour))
		if err != nil {
			return err
		}
		return writeJSON(map[string]any{"redacted": redacted, "retention_days": days})
	default:
		return usageError()
	}
}
