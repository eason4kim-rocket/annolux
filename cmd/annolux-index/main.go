package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eason4kim-rocket/annolux/discovery"
	"github.com/eason4kim-rocket/annolux/indexer"
	"github.com/eason4kim-rocket/annolux/searchindex"
)

func main() {
	seedsPath := flag.String("seeds", "", "JSON array of seed URLs")
	tier := flag.String("tier", "full", "write tier: full stores bodies, lite stores discovery sketches")
	priorityURLsPath := flag.String("priority-urls", "", "optional JSON array of exact URLs to lease first")
	outPath := flag.String("out", "./data/index.db", "index database path")
	maxPages := flag.Int("max-pages", 500, "stop after this many indexed pages")
	maxPerHost := flag.Int("max-per-host", 10000, "per-domain page cap")
	maxFrontierPerHost := flag.Int("max-frontier-per-host", 0, "per-domain frontier row budget for link discovery (0 = 2x max-per-host)")
	reopenStarved := flag.Int("reopen-starved", 0, "refetch closed rows of domains holding fewer frontier rows than this (0 = off)")
	pruneFrontier := flag.Bool("prune-frontier", false, "drop queued URLs the current admission rules reject")
	refreshExisting := flag.Bool("refresh-existing", false, "refresh only existing index rows; do not discover or mutate frontier")
	refreshOlderThan := flag.Duration("refresh-older-than", 7*24*time.Hour, "with -refresh-existing, select pages older than this duration")
	allowPrivate := flag.Bool("allow-private", false, "allow loopback/private seed hosts")
	maxDiskUsedPercent := flag.Int("max-disk-used-percent", indexer.DefaultMaxDiskUsedPercent, "stop when index filesystem use reaches this percentage")
	minDiskAvailableGiB := flag.Uint64("min-disk-available-gib", indexer.DefaultMinDiskAvailableBytes>>30, "stop when index filesystem available space falls below this many GiB")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *seedsPath, *priorityURLsPath, *outPath, *tier, *maxPages, *maxPerHost, *maxFrontierPerHost, *reopenStarved, *pruneFrontier, *refreshExisting, *refreshOlderThan, *allowPrivate, *maxDiskUsedPercent, *minDiskAvailableGiB); err != nil {
		fmt.Fprintf(os.Stderr, "annolux-index: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, seedsPath, priorityURLsPath, outPath, tier string, maxPages, maxPerHost, maxFrontierPerHost, reopenStarved int, pruneFrontier, refreshExisting bool, refreshOlderThan time.Duration, allowPrivate bool, maxDiskUsedPercent int, minDiskAvailableGiB uint64) error {
	if !refreshExisting && seedsPath == "" {
		return fmt.Errorf("-seeds is required")
	}
	if tier != "full" && tier != "lite" {
		return fmt.Errorf("-tier must be full or lite")
	}
	if tier == "lite" && refreshExisting {
		return fmt.Errorf("-tier=lite cannot combine with -refresh-existing")
	}
	var seeds []string
	var err error
	if seedsPath != "" {
		seeds, err = indexer.LoadSeeds(seedsPath)
		if err != nil {
			return err
		}
	}
	var priorityURLs []string
	if priorityURLsPath != "" {
		priorityURLs, err = indexer.LoadPriorityURLs(priorityURLsPath)
		if err != nil {
			return err
		}
	}
	capacityGuard, err := newDiskCapacityGuard(outPath, maxDiskUsedPercent, minDiskAvailableGiB, filesystemDiskUsage)
	if err != nil {
		return err
	}
	// Check before Open can create a directory, database, or WAL. Run checks
	// again before frontier mutations and continuously while workers execute.
	if err := capacityGuard(); err != nil {
		return err
	}
	store, err := searchindex.Open(outPath)
	if err != nil {
		return err
	}
	defer store.Close()
	fetcher, err := indexer.NewFetcher(indexer.FetcherConfig{AllowPrivateNetworks: allowPrivate})
	if err != nil {
		return err
	}
	if refreshExisting {
		stats, err := indexer.RefreshExisting(ctx, store, fetcher, indexer.RefreshConfig{
			MaxPages: maxPages, OlderThan: refreshOlderThan,
			AllowPrivate: allowPrivate, CapacityGuard: capacityGuard,
		})
		if err != nil {
			return err
		}
		fmt.Printf("selected=%d changed=%d unchanged=%d not_modified=%d failed=%d robots_deny=%d skipped_content=%d\n",
			stats.Selected, stats.Changed, stats.Unchanged, stats.NotModified, stats.Failed, stats.RobotsDeny, stats.SkippedContent)
		return nil
	}
	discoverer, err := discovery.NewService(discovery.Config{
		MaxURLs:              10000,
		UserAgent:            fetcher.UserAgent(),
		AllowPrivateNetworks: allowPrivate,
	})
	if err != nil {
		return err
	}
	stats, err := indexer.Run(ctx, store, fetcher, discoverer, seeds, indexer.RunConfig{
		MaxPages: maxPages, MaxPerHost: maxPerHost,
		MaxFrontierPerHost: maxFrontierPerHost,
		ReopenStarvedBelow: reopenStarved, PruneFrontier: pruneFrontier,
		AllowPrivate: allowPrivate, PriorityURLs: priorityURLs, CapacityGuard: capacityGuard,
		LiteTier: tier == "lite",
	})
	if err != nil {
		return err
	}
	fmt.Printf("discovered=%d indexed=%d failed=%d robots_deny=%d not_modified=%d skipped_content=%d\n",
		stats.Discovered, stats.Indexed, stats.Failed, stats.RobotsDeny, stats.Skipped304, stats.SkippedContent)
	return nil
}
