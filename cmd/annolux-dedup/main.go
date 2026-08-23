// Command annolux-dedup collapses exact-duplicate URLs: pages sharing one
// content_hash keep a single canonical row with the body and FTS postings,
// while every other address becomes a lightweight url_aliases row. The dry
// run doubles as the authoritative duplication measurement for a database.
//
// The default mode only reports. Applying requires an explicit -apply and is
// meant for a checkpointed candidate copy, never a database being served.
// Evaluation-target URLs passed via -watch-urls always win their group; a
// group holding several targets is skipped whole rather than collapsed.
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
)

var errUsage = errors.New("usage: annolux-dedup -db PATH [-apply] [-report PATH] [-watch-urls PATH] [-top N]")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "annolux-dedup: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, out io.Writer) error {
	flags := flag.NewFlagSet("annolux-dedup", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	databasePath := flags.String("db", "", "index database to measure or migrate")
	apply := flags.Bool("apply", false, "demote duplicate URLs to aliases; without it the run only reports")
	reportPath := flags.String("report", "", "also write the JSON report to this path")
	watchPath := flags.String("watch-urls", "", "file of URLs, one per line, that must stay canonical")
	topHosts := flags.Int("top", 25, "how many hosts to list in the report")
	timeout := flags.Duration("timeout", 2*time.Hour, "overall time budget")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *databasePath == "" || *topHosts < 0 {
		return errUsage
	}
	watch, err := indexops.ReadURLList(*watchPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	result, err := dedup(ctx, options{path: *databasePath, apply: *apply, topHosts: *topHosts, watch: watch})
	if err != nil {
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
