// Command annolux-indexclean applies the crawler's current junk rule
// retroactively to a stored search index: rows that the rule would reject
// today, but that entered before the rule existed, are deleted from pages,
// from the language FTS table they were indexed into, and from the frontier
// queue that would otherwise fetch them again.
//
// It never decides for itself what junk is. Every decision comes from
// indexer.JunkURL, the same function that admits new links.
//
// The default mode is a dry run that only reports. Deleting requires an
// explicit -apply, and the tool is meant to be pointed at a checkpointed
// copy of the index, never at a database a service is serving.
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

var errUsage = errors.New("usage: annolux-indexclean -db PATH [-apply] [-report PATH] [-top N] [-watch-urls PATH]")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "annolux-indexclean: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, out io.Writer) error {
	flags := flag.NewFlagSet("annolux-indexclean", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	databasePath := flags.String("db", "", "index database to inspect or clean")
	apply := flags.Bool("apply", false, "delete the reported rows; without it the run only reports")
	reportPath := flags.String("report", "", "also write the JSON report to this path")
	topHosts := flags.Int("top", 25, "how many hosts to list in the report")
	watchPath := flags.String("watch-urls", "", "file of URLs, one per line, to report the fate of individually")
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

	result, err := clean(ctx, options{path: *databasePath, apply: *apply, topHosts: *topHosts, watch: watch})
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
