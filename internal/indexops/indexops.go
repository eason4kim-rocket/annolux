// Package indexops holds the SQLite plumbing shared by the offline index
// maintenance tools (annolux-indexclean, annolux-dedup): safe read-only and
// writable opens, WAL discipline, integrity checks, and file hashing. The
// tools decide what to delete; this package only makes those decisions safe
// to execute and verify.
package indexops

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Prepare resolves and vets a database path: it must be an existing regular
// file with no uncheckpointed WAL, because both counting and deleting must
// see exactly what is on disk.
func Prepare(path string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("index must be an existing regular file")
	}
	walBytes, err := WALSize(absolute)
	if err != nil {
		return "", err
	}
	if walBytes > 0 {
		return "", fmt.Errorf("index WAL holds %d uncheckpointed bytes; checkpoint before operating", walBytes)
	}
	return absolute, nil
}

// Open opens the vetted file immutably for reporting, or read-write for an
// explicit apply. It deliberately skips the searchindex schema-version guard:
// maintenance tools must be able to inspect databases older or newer than
// the serving binary.
func Open(absolute string, writable bool) (*sql.DB, error) {
	query := url.Values{"_busy_timeout": []string{"5000"}}
	if writable {
		query.Set("_journal_mode", "wal")
		query.Set("_synchronous", "normal")
		query.Set("_txlock", "immediate")
	} else {
		query.Set("mode", "ro")
		query.Set("immutable", "1")
		query.Set("_pragma", "query_only(1)")
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute, RawQuery: query.Encode()}).String()
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

// WALSize reports the sidecar WAL size, zero when absent.
func WALSize(absolute string) (int64, error) {
	info, err := os.Stat(absolute + "-wal")
	switch {
	case err == nil:
		return info.Size(), nil
	case errors.Is(err, os.ErrNotExist):
		return 0, nil
	default:
		return 0, err
	}
}

// Checkpoint truncates the WAL so the main file is self-contained: the file
// is hashed, audited, and copied by path, and a database whose newest commits
// live only in a sidecar would be none of those.
func Checkpoint(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	if err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// IntegrityCheck runs SQLite's own structural check and fails on any finding.
func IntegrityCheck(ctx context.Context, database *sql.DB) (string, error) {
	rows, err := database.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return "", err
		}
		if result != "ok" {
			return "", fmt.Errorf("integrity check: %s", result)
		}
	}
	return "ok", rows.Err()
}

// FTSIntegrityCheck asks each contentless FTS table to verify its own
// inverted index; bulk retraction is exactly the operation that could leave a
// posting list pointing at a deleted rowid. It writes, so callers run it
// before the final checkpoint.
func FTSIntegrityCheck(ctx context.Context, database *sql.DB, tables ...string) (string, error) {
	for _, table := range tables {
		if _, err := database.ExecContext(ctx,
			`INSERT INTO `+table+`(`+table+`) VALUES('integrity-check')`,
		); err != nil {
			return "", fmt.Errorf("%s integrity: %w", table, err)
		}
	}
	return "ok", nil
}

// HashFile returns the file's SHA-256 for the operation report.
func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// ReadURLList loads newline-separated URLs, dropping blanks and duplicates,
// so a report can answer for each listed URL individually.
func ReadURLList(path string) ([]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	urls := make([]string, 0, 64)
	for _, line := range strings.Split(string(raw), "\n") {
		candidate := strings.TrimSpace(line)
		if candidate == "" {
			continue
		}
		if _, duplicate := seen[candidate]; duplicate {
			continue
		}
		seen[candidate] = struct{}{}
		urls = append(urls, candidate)
	}
	return urls, nil
}
