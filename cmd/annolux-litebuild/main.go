// Command annolux-litebuild constructs the discovery tier offline: it builds
// the document-frequency table from the stored full-text corpus, then
// creates one sketched, promoted discovery row per full-text page. Pointed at
// a candidate copy it gives the tiered index its first real discovery layer
// — and the first honest per-page cost measurement — without fetching a
// single page.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eason4kim-rocket/annolux/internal/indexops"
	"github.com/eason4kim-rocket/annolux/searchindex"
)

var errUsage = errors.New("usage: annolux-litebuild -db PATH [-skip-df] [-report PATH]")

type report struct {
	Path            string  `json:"path"`
	DFDocuments     int     `json:"df_documents"`
	DFSeconds       float64 `json:"df_seconds"`
	LitePages       int     `json:"lite_pages"`
	BackfillSeconds float64 `json:"backfill_seconds"`
	SizeBefore      int64   `json:"size_bytes_before"`
	SizeAfter       int64   `json:"size_bytes_after"`
	BytesPerPage    float64 `json:"lite_bytes_per_page"`
	Integrity       string  `json:"integrity"`
	FTSIntegrity    string  `json:"fts_integrity"`
	SHA256          string  `json:"sha256"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "annolux-litebuild: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, out io.Writer) error {
	flags := flag.NewFlagSet("annolux-litebuild", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	databasePath := flags.String("db", "", "candidate index database")
	skipDF := flags.Bool("skip-df", false, "reuse the existing document-frequency table")
	reportPath := flags.String("report", "", "also write the JSON report to this path")
	timeout := flags.Duration("timeout", 12*time.Hour, "overall time budget")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *databasePath == "" {
		return errUsage
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	absolute, err := indexops.Prepare(*databasePath)
	if err != nil {
		return err
	}
	result := report{Path: absolute}
	if info, err := os.Stat(absolute); err == nil {
		result.SizeBefore = info.Size()
	}
	store, err := searchindex.Open(absolute)
	if err != nil {
		return err
	}
	progress := func(stage string) func(int) {
		return func(done int) { _, _ = fmt.Fprintf(os.Stderr, "%s: %d\n", stage, done) }
	}
	if !*skipDF {
		started := time.Now()
		documents, err := store.BuildDFStats(ctx, progress("df"))
		if err != nil {
			_ = store.Close()
			return fmt.Errorf("build df stats: %w", err)
		}
		result.DFDocuments = documents
		result.DFSeconds = time.Since(started).Seconds()
	}
	started := time.Now()
	pages, err := store.BackfillLite(ctx, progress("lite"))
	if err != nil {
		_ = store.Close()
		return fmt.Errorf("backfill lite: %w", err)
	}
	result.LitePages = pages
	result.BackfillSeconds = time.Since(started).Seconds()
	if err := store.Close(); err != nil {
		return err
	}

	database, err := indexops.Open(absolute, true)
	if err != nil {
		return err
	}
	defer database.Close()
	if result.Integrity, err = indexops.IntegrityCheck(ctx, database); err != nil {
		return err
	}
	if result.FTSIntegrity, err = indexops.FTSIntegrityCheck(ctx, database,
		"pages_fts_en", "pages_fts_zh", "lite_fts_en", "lite_fts_zh"); err != nil {
		return err
	}
	if err := indexops.Checkpoint(ctx, database); err != nil {
		return err
	}
	if info, err := os.Stat(absolute); err == nil {
		result.SizeAfter = info.Size()
	}
	if result.LitePages > 0 {
		result.BytesPerPage = float64(result.SizeAfter-result.SizeBefore) / float64(result.LitePages)
	}
	if result.SHA256, err = indexops.HashFile(absolute); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if *reportPath != "" {
		if err := os.WriteFile(*reportPath, encoded, 0o600); err != nil {
			return err
		}
	}
	_, err = out.Write(encoded)
	return err
}
