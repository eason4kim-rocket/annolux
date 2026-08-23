package indexer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eason4kim-rocket/annolux/cleaner"
	"github.com/eason4kim-rocket/annolux/searchindex"
)

// RefreshConfig bounds a maintenance pass over already-indexed URLs.
type RefreshConfig struct {
	MaxPages      int
	OlderThan     time.Duration
	AllowPrivate  bool
	CapacityGuard CapacityGuard
}

// RefreshStats describes a no-discovery refresh pass.
type RefreshStats struct {
	Selected       int
	Changed        int
	Unchanged      int
	NotModified    int
	Failed         int
	RobotsDeny     int
	SkippedContent int
}

// RefreshExisting conditionally refetches only URLs already present in pages.
// It never reads, leases, or mutates frontier and never follows page links.
func RefreshExisting(ctx context.Context, store *searchindex.Store, fetcher *Fetcher, cfg RefreshConfig) (RefreshStats, error) {
	if store == nil || fetcher == nil {
		return RefreshStats{}, fmt.Errorf("indexer: store and fetcher are required")
	}
	if cfg.MaxPages <= 0 {
		cfg.MaxPages = defaultMaxPages
	}
	if cfg.OlderThan <= 0 {
		cfg.OlderThan = 7 * 24 * time.Hour
	}
	if cfg.CapacityGuard != nil {
		if err := cfg.CapacityGuard(); err != nil {
			return RefreshStats{}, err
		}
	}
	candidates, err := store.ListRefreshCandidates(ctx, time.Now().UTC().Add(-cfg.OlderThan), cfg.MaxPages)
	if err != nil {
		return RefreshStats{}, err
	}
	stats := RefreshStats{Selected: len(candidates)}
	pipeline := cleaner.NewCleaner()
	robotsCache := make(map[string]robotsEntry)
	lastFetch := make(map[string]time.Time)
	for _, item := range candidates {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if cfg.CapacityGuard != nil {
			if err := cfg.CapacityGuard(); err != nil {
				return stats, err
			}
		}
		policy, blocked := refreshRobots(ctx, fetcher, item.URL, robotsCache)
		if blocked || !policy.Allowed(fetcher.UserAgent(), item.URL) {
			stats.RobotsDeny++
			continue
		}
		if !waitRefreshHost(ctx, lastFetch, item.Root, policy.CrawlDelay(fetcher.UserAgent())) {
			return stats, ctx.Err()
		}
		fetched, fetchErr := fetcher.Get(ctx, item.URL, item.ETag, item.LastMod)
		if errors.Is(fetchErr, ErrNotModified) {
			if err := store.TouchPage(ctx, item.URL, fetched.FetchedAt, fetched.ETag, fetched.LastMod); err != nil {
				return stats, err
			}
			stats.NotModified++
			continue
		}
		if fetchErr != nil {
			stats.Failed++
			continue
		}
		if !indexableContentType(fetched.ContentType, fetched.Body) {
			stats.SkippedContent++
			continue
		}
		cleaned, cleanErr := pipeline.Clean(string(fetched.Body), fetched.URL, "text", "auto")
		if cleanErr != nil || cleaned == nil {
			stats.Failed++
			continue
		}
		page := searchindex.Page{
			// Keep the original key even if the server redirects. A refresh is
			// not permission to discover or insert a new canonical URL.
			URL: item.URL, Root: item.Root, Title: cleaned.Metadata.Title, Body: cleaned.Content,
			Lang:      searchindex.DetectLanguage(cleaned.Metadata.Title + " " + cleaned.Content),
			FetchedAt: fetched.FetchedAt, ETag: fetched.ETag, LastMod: fetched.LastMod,
			NeedsRender: NeedsRender(cleaned.Content),
		}
		page.ContentHash = searchindex.HashContent(page.Body)
		written, err := store.UpsertMany(ctx, []searchindex.Page{page})
		if err != nil {
			return stats, err
		}
		if written == 1 {
			stats.Changed++
		} else {
			stats.Unchanged++
		}
	}
	return stats, nil
}

func refreshRobots(ctx context.Context, fetcher *Fetcher, pageURL string, cache map[string]robotsEntry) (Robots, bool) {
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return Robots{}, true
	}
	host := strings.ToLower(parsed.Hostname())
	if entry, ok := cache[host]; ok {
		return entry.policy, entry.blocked
	}
	fetched, fetchErr := fetcher.Get(ctx, parsed.Scheme+"://"+parsed.Host+"/robots.txt", "", "")
	entry := robotsEntry{}
	switch {
	case fetchErr == nil:
		entry.policy = ParseRobots(strings.NewReader(string(fetched.Body)))
	case fetched.Status == http.StatusNotFound || fetched.Status == http.StatusGone:
	default:
		entry.blocked = true
	}
	cache[host] = entry
	return entry.policy, entry.blocked
}

func waitRefreshHost(ctx context.Context, last map[string]time.Time, root string, delay time.Duration) bool {
	wait := time.Duration(0)
	if previous := last[root]; !previous.IsZero() {
		wait = delay - time.Since(previous)
	}
	if wait > 0 && !sleepContext(ctx, wait) {
		return false
	}
	last[root] = time.Now()
	return true
}
