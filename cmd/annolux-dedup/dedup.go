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

	"github.com/eason4kim-rocket/annolux/internal/indexops"
	"github.com/eason4kim-rocket/annolux/searchindex"
)

// applyBatchGroups bounds how many duplicate groups one transaction retires,
// for the same reason the junk cleanup batches: one transaction over the
// whole migration would balloon the WAL, per-row transactions would fsync
// hundreds of thousands of times.
const applyBatchGroups = 1000

type options struct {
	path     string
	apply    bool
	topHosts int
	watch    []string
}

// pageRow is the slice of a pages row the migration decides with.
type pageRow struct {
	id        int64
	url       string
	lang      string
	fetchedAt int64
	etag      string
	lastMod   string
	urlBytes  int
}

// group is one content_hash with more than one URL: a winner keeps the body
// and postings, every loser becomes an alias.
type group struct {
	hash   string
	winner pageRow
	losers []pageRow
}

type hostCount struct {
	Host   string `json:"host"`
	Losers int64  `json:"losers"`
}

type snapshot struct {
	SizeBytes    int64 `json:"size_bytes"`
	Pages        int64 `json:"pages"`
	EnglishPages int64 `json:"english_pages"`
	ChinesePages int64 `json:"chinese_pages"`
	EnglishFTS   int64 `json:"english_fts"`
	ChineseFTS   int64 `json:"chinese_fts"`
	Aliases      int64 `json:"aliases"`
}

// report doubles as the corpus statistics deliverable: a dry run over a
// database is the authoritative duplication measurement for that database.
type report struct {
	Path string   `json:"path"`
	Mode string   `json:"mode"`
	Bef  snapshot `json:"before"`

	DistinctHashes  int64 `json:"distinct_hashes"`
	DuplicateGroups int64 `json:"duplicate_groups"`
	DuplicateURLs   int64 `json:"duplicate_urls"`
	DuplicateEN     int64 `json:"duplicate_urls_en"`
	DuplicateZH     int64 `json:"duplicate_urls_zh"`
	DistinctEN      int64 `json:"distinct_hashes_en"`
	DistinctZH      int64 `json:"distinct_hashes_zh"`

	URLBytesTotal int64 `json:"url_bytes_total"`
	URLBytesDupes int64 `json:"url_bytes_duplicates"`
	URLsOver500   int64 `json:"urls_over_500_bytes"`
	LongestURL    int64 `json:"longest_url_bytes"`

	WatchedProtected int64       `json:"watched_urls_protected"`
	SkippedGroups    int64       `json:"skipped_multi_watched_groups"`
	TopHosts         []hostCount `json:"top_hosts_by_losers"`

	AliasedURLs    int64     `json:"aliased_urls"`
	ReclaimedBytes int64     `json:"reclaimed_bytes"`
	After          *snapshot `json:"after,omitempty"`
	Integrity      string    `json:"integrity,omitempty"`
	FTSIntegrity   string    `json:"fts_integrity,omitempty"`
	SHA256         string    `json:"sha256,omitempty"`
}

func dedup(ctx context.Context, opts options) (report, error) {
	absolute, err := indexops.Prepare(opts.path)
	if err != nil {
		return report{}, err
	}
	// An apply first routes through searchindex.Open so the alias-table
	// migration lands exactly as a fresh index would create it; the raw
	// maintenance connection then does the bulk work.
	if opts.apply {
		store, openErr := searchindex.Open(absolute)
		if openErr != nil {
			return report{}, fmt.Errorf("migrate schema: %w", openErr)
		}
		if closeErr := store.Close(); closeErr != nil {
			return report{}, closeErr
		}
	}
	database, err := indexops.Open(absolute, opts.apply)
	if err != nil {
		return report{}, err
	}
	defer database.Close()

	result := report{Path: absolute, Mode: "dry-run"}
	if opts.apply {
		result.Mode = "apply"
	}
	if result.Bef, err = readSnapshot(ctx, database, absolute); err != nil {
		return report{}, err
	}
	groups, err := scan(ctx, database, opts.watch, &result)
	if err != nil {
		return report{}, err
	}
	result.TopHosts = topHosts(groups, opts.topHosts)
	if !opts.apply {
		return result, nil
	}
	aliased, err := applyGroups(ctx, database, groups)
	if err != nil {
		return report{}, err
	}
	result.AliasedURLs = aliased
	if aliased > 0 {
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
	result.ReclaimedBytes = result.Bef.SizeBytes - after.SizeBytes
	if err := verifyAfter(result); err != nil {
		return report{}, err
	}
	if result.SHA256, err = indexops.HashFile(absolute); err != nil {
		return report{}, err
	}
	return result, nil
}

// scan reads every page ordered by content hash, measures the URL byte
// distribution, and assembles the duplicate groups with their canonical
// winner already chosen.
func scan(ctx context.Context, database *sql.DB, watch []string, result *report) ([]group, error) {
	watched := make(map[string]struct{}, len(watch))
	for _, candidate := range watch {
		watched[candidate] = struct{}{}
	}
	rows, err := database.QueryContext(ctx, `
		SELECT id, url, lang, content_hash, fetched_at, etag, last_mod
		FROM pages ORDER BY content_hash, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]group, 0, 1024)
	var current []pageRow
	var currentHash string
	langOf := map[string]string{}

	flush := func() {
		if currentHash == "" {
			return
		}
		result.DistinctHashes++
		if langOf[currentHash] == searchindex.LangChinese {
			result.DistinctZH++
		} else {
			result.DistinctEN++
		}
		if len(current) < 2 {
			current = nil
			return
		}
		assembled, skipped, protected := assembleGroup(currentHash, current, watched)
		result.WatchedProtected += protected
		if skipped {
			result.SkippedGroups++
			current = nil
			return
		}
		result.DuplicateGroups++
		for _, loser := range assembled.losers {
			result.DuplicateURLs++
			result.URLBytesDupes += int64(loser.urlBytes)
			if loser.lang == searchindex.LangChinese {
				result.DuplicateZH++
			} else {
				result.DuplicateEN++
			}
		}
		groups = append(groups, assembled)
		current = nil
	}

	for rows.Next() {
		var row pageRow
		var lang, hash string
		if err := rows.Scan(&row.id, &row.url, &lang, &hash, &row.fetchedAt, &row.etag, &row.lastMod); err != nil {
			return nil, err
		}
		row.lang = lang
		row.urlBytes = len(row.url)
		result.URLBytesTotal += int64(row.urlBytes)
		if row.urlBytes > 500 {
			result.URLsOver500++
		}
		if int64(row.urlBytes) > result.LongestURL {
			result.LongestURL = int64(row.urlBytes)
		}
		if hash != currentHash {
			flush()
			currentHash = hash
		}
		langOf[currentHash] = lang
		current = append(current, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	flush()
	return groups, nil
}

// assembleGroup picks the canonical winner. A watched URL — an evaluation
// target — always wins its group; a group holding several watched URLs is
// left untouched entirely, because collapsing it would delete a URL some
// frozen test case must still be able to retrieve.
func assembleGroup(hash string, members []pageRow, watched map[string]struct{}) (group, bool, int64) {
	var protected int64
	watchedIndex := -1
	for index, member := range members {
		if _, hot := watched[member.url]; hot {
			protected++
			if watchedIndex >= 0 {
				return group{}, true, protected
			}
			watchedIndex = index
		}
	}
	winner := 0
	if watchedIndex >= 0 {
		winner = watchedIndex
	} else {
		for index := 1; index < len(members); index++ {
			if canonicalPreferred(members[index], members[winner]) {
				winner = index
			}
		}
	}
	assembled := group{hash: hash, winner: members[winner]}
	for index, member := range members {
		if index != winner {
			assembled.losers = append(assembled.losers, member)
		}
	}
	return assembled, false, protected
}

// canonicalPreferred reports whether a should be the canonical URL over b.
// The rules are deliberately boring and total: HTTPS over HTTP, fewer path
// segments, no query string, shorter, then lexicographic.
func canonicalPreferred(a, b pageRow) bool {
	aParsed, aErr := url.Parse(a.url)
	bParsed, bErr := url.Parse(b.url)
	if aErr != nil || bErr != nil {
		return aErr == nil && bErr != nil
	}
	if aHTTPS, bHTTPS := aParsed.Scheme == "https", bParsed.Scheme == "https"; aHTTPS != bHTTPS {
		return aHTTPS
	}
	if aSegments, bSegments := pathSegments(aParsed), pathSegments(bParsed); aSegments != bSegments {
		return aSegments < bSegments
	}
	if aQuery, bQuery := aParsed.RawQuery != "", bParsed.RawQuery != ""; aQuery != bQuery {
		return !aQuery
	}
	if len(a.url) != len(b.url) {
		return len(a.url) < len(b.url)
	}
	return a.url < b.url
}

func pathSegments(parsed *url.URL) int {
	trimmed := strings.Trim(parsed.EscapedPath(), "/")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "/") + 1
}

// applyGroups demotes every loser to an alias in bounded transactions.
func applyGroups(ctx context.Context, database *sql.DB, groups []group) (int64, error) {
	var aliased int64
	for start := 0; start < len(groups); start += applyBatchGroups {
		end := min(start+applyBatchGroups, len(groups))
		count, err := applyBatch(ctx, database, groups[start:end])
		if err != nil {
			return aliased, err
		}
		aliased += count
	}
	return aliased, nil
}

func applyBatch(ctx context.Context, database *sql.DB, batch []group) (int64, error) {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var aliased int64
	for _, item := range batch {
		for _, loser := range item.losers {
			table := "pages_fts_en"
			if loser.lang == searchindex.LangChinese {
				table = "pages_fts_zh"
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE rowid=?`, loser.id); err != nil {
				return 0, fmt.Errorf("delete %s rowid %d: %w", table, loser.id, err)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE id=?`, loser.id); err != nil {
				return 0, fmt.Errorf("delete page %d: %w", loser.id, err)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO url_aliases(url, page_id, fetched_at, etag, last_mod)
				VALUES (?,?,?,?,?)`,
				loser.url, item.winner.id, loser.fetchedAt, loser.etag, loser.lastMod,
			); err != nil {
				return 0, fmt.Errorf("insert alias %q: %w", loser.url, err)
			}
			aliased++
		}
	}
	return aliased, tx.Commit()
}

func readSnapshot(ctx context.Context, database *sql.DB, absolute string) (snapshot, error) {
	var current snapshot
	info, err := os.Stat(absolute)
	if err != nil {
		return snapshot{}, err
	}
	current.SizeBytes = info.Size()
	counts := []struct {
		query string
		value *int64
	}{
		{`SELECT COUNT(*) FROM pages`, &current.Pages},
		{`SELECT COUNT(*) FROM pages WHERE lang='en'`, &current.EnglishPages},
		{`SELECT COUNT(*) FROM pages WHERE lang='zh'`, &current.ChinesePages},
		{`SELECT COUNT(*) FROM pages_fts_en`, &current.EnglishFTS},
		{`SELECT COUNT(*) FROM pages_fts_zh`, &current.ChineseFTS},
	}
	for _, count := range counts {
		if err := database.QueryRowContext(ctx, count.query).Scan(count.value); err != nil {
			return snapshot{}, fmt.Errorf("%s: %w", count.query, err)
		}
	}
	// url_aliases only exists after the migration; a dry run on an older
	// database reports zero.
	err = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM url_aliases`).Scan(&current.Aliases)
	if err != nil && !isMissingTable(err) {
		return snapshot{}, err
	}
	return current, nil
}

func isMissingTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}

func verifyAfter(result report) error {
	after := result.After
	switch {
	case after == nil:
		return errors.New("apply produced no post-state")
	case after.Pages != result.Bef.Pages-result.AliasedURLs:
		return fmt.Errorf("expected %d pages, found %d", result.Bef.Pages-result.AliasedURLs, after.Pages)
	case after.Aliases != result.Bef.Aliases+result.AliasedURLs:
		return errors.New("alias count does not match the migration plan")
	case after.EnglishPages != after.EnglishFTS || after.ChinesePages != after.ChineseFTS:
		return errors.New("FTS row counts do not match page counts")
	case after.Pages != after.EnglishPages+after.ChinesePages:
		return errors.New("page language counts do not sum to the page count")
	case after.EnglishPages != result.Bef.EnglishPages-result.DuplicateEN:
		return errors.New("English page count does not match the plan")
	case after.ChinesePages != result.Bef.ChinesePages-result.DuplicateZH:
		return errors.New("Chinese page count does not match the plan")
	}
	return nil
}

func topHosts(groups []group, limit int) []hostCount {
	counts := map[string]int64{}
	for _, item := range groups {
		for _, loser := range item.losers {
			counts[hostOf(loser.url)]++
		}
	}
	ranked := make([]hostCount, 0, len(counts))
	for host, count := range counts {
		ranked = append(ranked, hostCount{Host: host, Losers: count})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Losers != ranked[j].Losers {
			return ranked[i].Losers > ranked[j].Losers
		}
		return ranked[i].Host < ranked[j].Host
	})
	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}

func hostOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return "(unparsable)"
	}
	return strings.ToLower(parsed.Hostname())
}
