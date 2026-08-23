package main

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

	_ "modernc.org/sqlite"
)

type indexAudit struct {
	Path           string         `json:"path"`
	SizeBytes      int64          `json:"size_bytes"`
	SHA256         string         `json:"sha256"`
	Integrity      string         `json:"integrity"`
	Pages          int64          `json:"pages"`
	EnglishPages   int64          `json:"english_pages"`
	ChinesePages   int64          `json:"chinese_pages"`
	EnglishFTS     int64          `json:"english_fts"`
	ChineseFTS     int64          `json:"chinese_fts"`
	LatestFetched  int64          `json:"latest_fetched_at"`
	FrontierStates map[string]int `json:"frontier_states"`
}

func auditIndex(ctx context.Context, path string) (indexAudit, error) {
	if ctx == nil || path == "" {
		return indexAudit{}, errors.New("index path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return indexAudit{}, err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return indexAudit{}, errors.New("index must be a regular file")
	}
	if wal, walErr := os.Stat(absolute + "-wal"); walErr == nil && wal.Size() > 0 {
		return indexAudit{}, fmt.Errorf("index WAL is not empty (%d bytes); checkpoint before copying", wal.Size())
	} else if walErr != nil && !errors.Is(walErr, os.ErrNotExist) {
		return indexAudit{}, walErr
	}
	digest, err := hashFile(absolute)
	if err != nil {
		return indexAudit{}, err
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute, RawQuery: url.Values{
		"mode":    []string{"ro"},
		"_pragma": []string{"query_only(1)"},
	}.Encode()}).String()
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return indexAudit{}, err
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	audit := indexAudit{Path: absolute, SizeBytes: info.Size(), SHA256: digest, FrontierStates: map[string]int{}}
	rows, err := database.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return indexAudit{}, err
	}
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			rows.Close()
			return indexAudit{}, err
		}
		if result != "ok" {
			rows.Close()
			return indexAudit{}, fmt.Errorf("index integrity check: %s", result)
		}
		audit.Integrity = result
	}
	if err := rows.Close(); err != nil {
		return indexAudit{}, err
	}
	queries := []struct {
		query string
		value *int64
	}{
		{`SELECT COUNT(*) FROM pages`, &audit.Pages},
		{`SELECT COUNT(*) FROM pages WHERE lang='en'`, &audit.EnglishPages},
		{`SELECT COUNT(*) FROM pages WHERE lang='zh'`, &audit.ChinesePages},
		{`SELECT COUNT(*) FROM pages_fts_en`, &audit.EnglishFTS},
		{`SELECT COUNT(*) FROM pages_fts_zh`, &audit.ChineseFTS},
		{`SELECT COALESCE(MAX(fetched_at),0) FROM pages`, &audit.LatestFetched},
	}
	for _, query := range queries {
		if err := database.QueryRowContext(ctx, query.query).Scan(query.value); err != nil {
			return indexAudit{}, err
		}
	}
	frontierRows, err := database.QueryContext(ctx, `SELECT state,COUNT(*) FROM frontier GROUP BY state ORDER BY state`)
	if err != nil {
		return indexAudit{}, err
	}
	for frontierRows.Next() {
		var state string
		var count int
		if err := frontierRows.Scan(&state, &count); err != nil {
			frontierRows.Close()
			return indexAudit{}, err
		}
		audit.FrontierStates[state] = count
	}
	if err := frontierRows.Close(); err != nil {
		return indexAudit{}, err
	}
	if audit.Integrity != "ok" || audit.Pages != audit.EnglishPages+audit.ChinesePages || audit.EnglishPages != audit.EnglishFTS || audit.ChinesePages != audit.ChineseFTS {
		return indexAudit{}, errors.New("index page, language, and FTS counts do not agree")
	}
	return audit, nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
