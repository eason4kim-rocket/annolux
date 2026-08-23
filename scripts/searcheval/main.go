// Command searcheval runs a frozen target-retrieval corpus against one Annolux
// Search API and writes a self-describing JSONL round exactly once.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/eason4kim-rocket/annolux/internal/searcheval"
)

var errUsage = errors.New("searcheval: invalid invocation")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "searcheval: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	if ctx == nil {
		return errUsage
	}
	flags := flag.NewFlagSet("searcheval", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	manifestPath := flags.String("manifest", "", "frozen corpus manifest")
	outputPath := flags.String("out", "", "new JSONL round path")
	apiURL := flags.String("api-url", "", "Annolux API base URL without credentials")
	runID := flags.String("run-id", "", "immutable round identifier")
	codeCommit := flags.String("code-commit", "", "deployed source commit")
	evaluatorCommit := flags.String("evaluator-commit", "", "evaluation runner source commit")
	imageDigest := flags.String("image-digest", "", "deployed image digest")
	split := flags.String("split", "all", "all, dev, or heldout")
	timeout := flags.Duration("timeout", 5*time.Second, "per-query HTTP timeout")
	indexPages := flags.Int64("index-pages", 0, "indexed page count")
	indexEnglish := flags.Int64("index-en", 0, "English page count")
	indexChinese := flags.Int64("index-zh", 0, "Chinese page count")
	indexLatest := flags.Int64("index-latest-fetched-at", 0, "latest indexed fetched_at Unix time")
	indexDBBytes := flags.Int64("index-db-bytes", 0, "SQLite database bytes")
	indexWALBytes := flags.Int64("index-wal-bytes", 0, "SQLite WAL bytes")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 ||
		*manifestPath == "" || *outputPath == "" || *apiURL == "" || *runID == "" ||
		*codeCommit == "" || *evaluatorCommit == "" || *imageDigest == "" {
		return errUsage
	}
	apiKey := os.Getenv("ANNOLUX_API_KEY")
	if apiKey == "" {
		return errors.New("ANNOLUX_API_KEY is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parentInfo, err := os.Stat(filepath.Dir(*outputPath))
	if err != nil || !parentInfo.IsDir() {
		return errors.New("output directory must already exist")
	}
	corpus, err := searcheval.LoadCorpus(*manifestPath)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: *timeout}
	report, err := searcheval.Evaluate(ctx, client, corpus, searcheval.RunOptions{
		RunID: *runID, APIURL: *apiURL, APIKey: apiKey, CodeCommit: *codeCommit, EvaluatorCommit: *evaluatorCommit,
		ImageDigest: *imageDigest,
		Split:       *split, Timeout: *timeout,
		Index: searcheval.IndexIdentity{
			Pages: *indexPages, EnglishPages: *indexEnglish, ChinesePages: *indexChinese,
			LatestFetchedAt: *indexLatest, DatabaseBytes: *indexDBBytes, WALBytes: *indexWALBytes,
		},
	})
	if err != nil {
		return err
	}
	encoded, err := searcheval.EncodeJSONL(report)
	if err != nil {
		return err
	}
	if err := searcheval.WriteExclusive(*outputPath, encoded); err != nil {
		return err
	}
	summary, err := jsonSummary(report)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(summary)
	return err
}

func jsonSummary(report searcheval.RunReport) ([]byte, error) {
	// Keep stdout machine-readable without duplicating the per-case artifact.
	return appendJSONNewline(report.Summary)
}

func appendJSONNewline(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}
