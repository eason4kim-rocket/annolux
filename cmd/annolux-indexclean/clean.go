package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/eason4kim-rocket/annolux/indexer"
	"github.com/eason4kim-rocket/annolux/internal/indexops"
	"github.com/eason4kim-rocket/annolux/searchindex"
)

// deleteBatchSize bounds how many pages one transaction retires. A single
// transaction over the whole cleanup would grow the WAL past the database it
// is cleaning; a per-row transaction would fsync a hundred thousand times.
const deleteBatchSize = 2000

// options describes one cleanup invocation. The zero value is a dry run,
// which is the only mode that may ever be reached by accident.
type options struct {
	path     string
	apply    bool
	topHosts int
	watch    []string
}

// snapshot is the set of counts the cleanup is judged by, taken before and
// after the deletions.
type snapshot struct {
	SizeBytes    int64            `json:"size_bytes"`
	WALBytes     int64            `json:"wal_bytes"`
	Pages        int64            `json:"pages"`
	EnglishPages int64            `json:"english_pages"`
	ChinesePages int64            `json:"chinese_pages"`
	EnglishFTS   int64            `json:"english_fts"`
	ChineseFTS   int64            `json:"chinese_fts"`
	Frontier     int64            `json:"frontier"`
	FrontierBy   map[string]int64 `json:"frontier_states"`
}

type hostCount struct {
	Host      string `json:"host"`
	Pages     int64  `json:"pages"`
	Junk      int64  `json:"junk"`
	EmptyBody int64  `json:"empty_body"`
}

// watched is one URL the operator asked about by name — a corpus target,
// usually — and whether this cleanup would remove it. A cleanup that removes
// an evaluation target changes what the gate can possibly measure, so the
// answer has to be available before the deletions, not after.
type watched struct {
	URL       string `json:"url"`
	Indexed   bool   `json:"indexed"`
	Doomed    bool   `json:"doomed"`
	Junk      bool   `json:"junk"`
	EmptyBody bool   `json:"empty_body"`
	Reason    string `json:"reason,omitempty"`
}

// report is the full account of one run: what was found, what was removed,
// and what the database looked like on either side of the removal.
type report struct {
	Path              string           `json:"path"`
	Mode              string           `json:"mode"`
	Before            snapshot         `json:"before"`
	After             *snapshot        `json:"after,omitempty"`
	JunkPages         int64            `json:"junk_pages"`
	JunkPagesByReason map[string]int64 `json:"junk_pages_by_reason"`
	JunkEnglish       int64            `json:"junk_english_pages"`
	JunkChinese       int64            `json:"junk_chinese_pages"`
	EmptyBodyPages    int64            `json:"empty_body_pages"`
	EmptyBodyOnly     int64            `json:"empty_body_pages_not_junk"`
	EmptyBodyEnglish  int64            `json:"empty_body_english_pages"`
	EmptyBodyChinese  int64            `json:"empty_body_chinese_pages"`
	DoomedPages       int64            `json:"doomed_pages"`
	DoomedEnglish     int64            `json:"doomed_english_pages"`
	DoomedChinese     int64            `json:"doomed_chinese_pages"`
	JunkFrontier      int64            `json:"junk_frontier_rows"`
	TopHosts          []hostCount      `json:"top_hosts"`
	WatchedURLs       []watched        `json:"watched_urls,omitempty"`
	WatchedDoomed     int64            `json:"watched_urls_doomed"`
	DeletedPages      int64            `json:"deleted_pages"`
	DeletedFrontier   int64            `json:"deleted_frontier_rows"`
	ReclaimedBytes    int64            `json:"reclaimed_bytes"`
	Integrity         string           `json:"integrity,omitempty"`
	FTSIntegrity      string           `json:"fts_integrity,omitempty"`
	SHA256            string           `json:"sha256,omitempty"`
}

// doomed is one page the cleanup will delete, carrying only what the delete
// and the report need.
type doomed struct {
	id   int64
	lang string
}

func clean(ctx context.Context, opts options) (report, error) {
	absolute, err := indexops.Prepare(opts.path)
	if err != nil {
		return report{}, err
	}
	database, err := indexops.Open(absolute, opts.apply)
	if err != nil {
		return report{}, err
	}
	defer database.Close()

	result := report{Path: absolute, Mode: "dry-run", JunkPagesByReason: map[string]int64{}}
	if opts.apply {
		result.Mode = "apply"
	}
	if result.Before, err = readSnapshot(ctx, database, absolute); err != nil {
		return report{}, err
	}
	condemned, hosts, err := scanPages(ctx, database, opts.watch, &result)
	if err != nil {
		return report{}, err
	}
	result.TopHosts = topHosts(hosts, opts.topHosts)
	frontierURLs, err := scanFrontier(ctx, database)
	if err != nil {
		return report{}, err
	}
	result.JunkFrontier = int64(len(frontierURLs))
	if !opts.apply {
		return result, nil
	}
	if err := deletePages(ctx, database, condemned); err != nil {
		return report{}, err
	}
	result.DeletedPages = int64(len(condemned))
	if err := deleteFrontier(ctx, database, frontierURLs); err != nil {
		return report{}, err
	}
	result.DeletedFrontier = int64(len(frontierURLs))
	// Vacuuming is what reclaims the freed pages, and it is also the most
	// expensive step here, so a pass that deleted nothing skips it.
	if result.DeletedPages > 0 || result.DeletedFrontier > 0 {
		if _, err := database.ExecContext(ctx, `VACUUM`); err != nil {
			return report{}, fmt.Errorf("vacuum: %w", err)
		}
	}
	if result.Integrity, err = indexops.IntegrityCheck(ctx, database); err != nil {
		return report{}, err
	}
	if result.FTSIntegrity, err = indexops.FTSIntegrityCheck(ctx, database, "pages_fts_en", "pages_fts_zh"); err != nil {
		return report{}, err
	}
	if err := indexops.Checkpoint(ctx, database); err != nil {
		return report{}, err
	}
	after, err := readSnapshot(ctx, database, absolute)
	if err != nil {
		return report{}, err
	}
	result.After = &after
	result.ReclaimedBytes = result.Before.SizeBytes - after.SizeBytes
	if err := verifyAfter(result); err != nil {
		return report{}, err
	}
	if result.SHA256, err = indexops.HashFile(absolute); err != nil {
		return report{}, err
	}
	return result, nil
}

func readSnapshot(ctx context.Context, database *sql.DB, absolute string) (snapshot, error) {
	current := snapshot{FrontierBy: map[string]int64{}}
	info, err := os.Stat(absolute)
	if err != nil {
		return snapshot{}, err
	}
	current.SizeBytes = info.Size()
	if current.WALBytes, err = indexops.WALSize(absolute); err != nil {
		return snapshot{}, err
	}
	counts := []struct {
		query string
		value *int64
	}{
		{`SELECT COUNT(*) FROM pages`, &current.Pages},
		{`SELECT COUNT(*) FROM pages WHERE lang='en'`, &current.EnglishPages},
		{`SELECT COUNT(*) FROM pages WHERE lang='zh'`, &current.ChinesePages},
		{`SELECT COUNT(*) FROM pages_fts_en`, &current.EnglishFTS},
		{`SELECT COUNT(*) FROM pages_fts_zh`, &current.ChineseFTS},
		{`SELECT COUNT(*) FROM frontier`, &current.Frontier},
	}
	for _, count := range counts {
		if err := database.QueryRowContext(ctx, count.query).Scan(count.value); err != nil {
			return snapshot{}, fmt.Errorf("%s: %w", count.query, err)
		}
	}
	rows, err := database.QueryContext(ctx, `SELECT state,COUNT(*) FROM frontier GROUP BY state ORDER BY state`)
	if err != nil {
		return snapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return snapshot{}, err
		}
		current.FrontierBy[state] = count
	}
	return current, rows.Err()
}

// scanPages walks every stored page once, asking the crawler's own rule which
// rows would not be admitted today, and collects the empty-body rows that
// carry no retrievable text at all.
func scanPages(ctx context.Context, database *sql.DB, watch []string, result *report) ([]doomed, map[string]*hostCount, error) {
	rows, err := database.QueryContext(ctx, `SELECT id, url, lang, COALESCE(LENGTH(body_z),0) FROM pages ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	condemned := make([]doomed, 0, 1024)
	hosts := map[string]*hostCount{}
	watching := make(map[string]*watched, len(watch))
	for _, candidate := range watch {
		watching[candidate] = &watched{URL: candidate}
	}
	for rows.Next() {
		var id int64
		var pageURL, lang string
		var bodyBytes int64
		if err := rows.Scan(&id, &pageURL, &lang, &bodyBytes); err != nil {
			return nil, nil, err
		}
		chinese := lang == searchindex.LangChinese
		junk := indexer.JunkURL(pageURL)
		empty := bodyBytes == 0
		if junk {
			result.JunkPages++
			result.JunkPagesByReason[indexer.JunkReason(pageURL)]++
			addLanguage(chinese, &result.JunkEnglish, &result.JunkChinese)
		}
		if empty {
			result.EmptyBodyPages++
			addLanguage(chinese, &result.EmptyBodyEnglish, &result.EmptyBodyChinese)
			if !junk {
				result.EmptyBodyOnly++
			}
		}
		if tracked, ok := watching[pageURL]; ok {
			tracked.Indexed = true
			tracked.Doomed = junk || empty
			tracked.Junk = junk
			tracked.EmptyBody = empty
			tracked.Reason = indexer.JunkReason(pageURL)
		}
		if !junk && !empty {
			continue
		}
		condemned = append(condemned, doomed{id: id, lang: lang})
		addLanguage(chinese, &result.DoomedEnglish, &result.DoomedChinese)
		host := hosts[hostOf(pageURL)]
		if host == nil {
			host = &hostCount{Host: hostOf(pageURL)}
			hosts[host.Host] = host
		}
		host.Pages++
		if junk {
			host.Junk++
		}
		if empty {
			host.EmptyBody++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	result.DoomedPages = int64(len(condemned))
	for _, candidate := range watch {
		tracked := watching[candidate]
		result.WatchedURLs = append(result.WatchedURLs, *tracked)
		if tracked.Doomed {
			result.WatchedDoomed++
		}
	}
	return condemned, hosts, nil
}

func addLanguage(chinese bool, english, mandarin *int64) {
	if chinese {
		*mandarin++
		return
	}
	*english++
}

// scanFrontier finds queued URLs the same rule rejects, so the next crawl
// does not simply fetch the cleaned pages back.
func scanFrontier(ctx context.Context, database *sql.DB) ([]string, error) {
	rows, err := database.QueryContext(ctx, `SELECT url FROM frontier`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	junk := make([]string, 0, 256)
	for rows.Next() {
		var queued string
		if err := rows.Scan(&queued); err != nil {
			return nil, err
		}
		if indexer.JunkURL(queued) {
			junk = append(junk, queued)
		}
	}
	return junk, rows.Err()
}

func deletePages(ctx context.Context, database *sql.DB, condemned []doomed) error {
	for start := 0; start < len(condemned); start += deleteBatchSize {
		end := min(start+deleteBatchSize, len(condemned))
		if err := deletePageBatch(ctx, database, condemned[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func deletePageBatch(ctx context.Context, database *sql.DB, batch []doomed) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, page := range batch {
		// The FTS tables are contentless with contentless_delete=1, and their
		// rowid is the page id, so a retraction is a plain delete by rowid on
		// the table the page's language was indexed into.
		table := "pages_fts_en"
		if page.lang == searchindex.LangChinese {
			table = "pages_fts_zh"
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE rowid=?`, page.id); err != nil {
			return fmt.Errorf("delete %s rowid %d: %w", table, page.id, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE id=?`, page.id); err != nil {
			return fmt.Errorf("delete page %d: %w", page.id, err)
		}
	}
	return tx.Commit()
}

func deleteFrontier(ctx context.Context, database *sql.DB, urls []string) error {
	for start := 0; start < len(urls); start += deleteBatchSize {
		end := min(start+deleteBatchSize, len(urls))
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, queued := range urls[start:end] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM frontier WHERE url=?`, queued); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("delete frontier row: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// verifyAfter refuses to call a cleanup successful unless the surviving
// database is internally consistent and lost exactly the rows that were
// condemned — an FTS table left out of step with pages would serve
// hits for documents the index can no longer return.
func verifyAfter(result report) error {
	after := result.After
	switch {
	case after == nil:
		return errors.New("cleanup produced no post-state")
	case after.Pages != after.EnglishPages+after.ChinesePages:
		return errors.New("page language counts do not sum to the page count")
	case after.EnglishPages != after.EnglishFTS || after.ChinesePages != after.ChineseFTS:
		return errors.New("FTS row counts do not match page counts")
	case after.Pages != result.Before.Pages-result.DoomedPages:
		return fmt.Errorf("expected %d pages, found %d", result.Before.Pages-result.DoomedPages, after.Pages)
	case after.EnglishPages != result.Before.EnglishPages-result.DoomedEnglish:
		return errors.New("English page count does not match the deletion plan")
	case after.ChinesePages != result.Before.ChinesePages-result.DoomedChinese:
		return errors.New("Chinese page count does not match the deletion plan")
	case after.Frontier != result.Before.Frontier-result.DeletedFrontier:
		return errors.New("frontier count does not match the deletion plan")
	case after.WALBytes > 0:
		return errors.New("cleaned index still has an uncheckpointed WAL")
	}
	return nil
}

func hostOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return "(unparsable)"
	}
	return strings.ToLower(parsed.Hostname())
}

func topHosts(hosts map[string]*hostCount, limit int) []hostCount {
	ranked := make([]hostCount, 0, len(hosts))
	for _, count := range hosts {
		ranked = append(ranked, *count)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Pages != ranked[j].Pages {
			return ranked[i].Pages > ranked[j].Pages
		}
		return ranked[i].Host < ranked[j].Host
	})
	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}
