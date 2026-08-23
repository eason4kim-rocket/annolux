package searchindex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// LitePage is one discovery-tier document: enough to find the page and judge
// whether it deserves promotion, nothing more.
type LitePage struct {
	URL         string
	Root        string
	Title       string
	Headings    string
	Summary     string
	Body        string // sketched, never stored
	Lang        string
	FetchedAt   time.Time
	ContentHash string
	Promoted    bool
}

// LiteHit is one ranked discovery-tier result.
type LiteHit struct {
	URL       string
	Title     string
	Root      string
	Summary   string
	FetchedAt int64
	Score     float64
	Lang      string
}

// liteBatchSize bounds one backfill transaction, matching the maintenance
// tools' batching rationale.
const liteBatchSize = 500

// UpsertLiteMany writes discovery-tier pages in one transaction, sketching
// each body against the stored document frequencies. Bodies are reduced to
// their sketch and never persisted.
func (s *Store) UpsertLiteMany(ctx context.Context, pages []LitePage) (int, error) {
	if err := s.guard(ctx); err != nil {
		return 0, err
	}
	if len(pages) == 0 {
		return 0, nil
	}
	frequencies, err := s.LoadDocumentFrequencies(ctx)
	if err != nil {
		return 0, err
	}
	s.gate.RLock()
	defer s.gate.RUnlock()
	if s.closed {
		return 0, ErrClosed
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("searchindex: begin lite batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	written := 0
	for _, page := range pages {
		changed, err := upsertLiteTx(ctx, tx, page, frequenciesFor(frequencies, page.Lang))
		if err != nil {
			return 0, err
		}
		if changed {
			written++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("searchindex: commit lite batch: %w", err)
	}
	return written, nil
}

func upsertLiteTx(ctx context.Context, tx *sql.Tx, page LitePage, frequencies DocumentFrequencies) (bool, error) {
	page.URL = strings.TrimSpace(page.URL)
	page.Root = strings.TrimSpace(page.Root)
	page.Lang = strings.TrimSpace(page.Lang)
	if page.URL == "" || page.Root == "" {
		return false, fmt.Errorf("%w: lite url and root are required", ErrInvalidPage)
	}
	if page.Lang != LangEnglish && page.Lang != LangChinese {
		return false, fmt.Errorf("%w: lite lang must be en or zh", ErrInvalidPage)
	}
	if page.FetchedAt.IsZero() {
		page.FetchedAt = time.Now().UTC()
	}
	if page.ContentHash == "" {
		page.ContentHash = HashContent(page.Body)
	}
	if page.Summary == "" {
		page.Summary = buildLead(page.Title, page.Body)
	}
	// A URL whose body already lives in the full-text core — as a page or as
	// an alias of one — must not also surface from the discovery lane, so its
	// lite row is forced to promoted regardless of what the caller knew.
	if !page.Promoted {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM pages WHERE url=?`, page.URL).Scan(&one)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("searchindex: lite promotion probe: %w", err)
		}
		if err == nil {
			page.Promoted = true
		} else {
			err = tx.QueryRowContext(ctx, `SELECT 1 FROM url_aliases WHERE url=?`, page.URL).Scan(&one)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return false, fmt.Errorf("searchindex: lite alias probe: %w", err)
			}
			page.Promoted = err == nil
		}
	}

	var existingID int64
	var existingHash, existingLang string
	err := tx.QueryRowContext(ctx, `SELECT id, content_hash, lang FROM pages_lite WHERE url = ?`, page.URL).
		Scan(&existingID, &existingHash, &existingLang)
	switch {
	case err == nil && existingHash == page.ContentHash:
		if _, err := tx.ExecContext(ctx,
			`UPDATE pages_lite SET fetched_at=?, promoted=? WHERE id=?`,
			page.FetchedAt.Unix(), boolToInt(page.Promoted), existingID,
		); err != nil {
			return false, fmt.Errorf("searchindex: refresh lite metadata: %w", err)
		}
		return false, nil
	case err == nil:
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+liteFTSTable(existingLang)+` WHERE rowid = ?`, existingID); err != nil {
			return false, fmt.Errorf("searchindex: retract lite fts: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE pages_lite SET root=?, title=?, summary=?, lang=?, fetched_at=?, content_hash=?, promoted=?
			WHERE id=?`,
			page.Root, page.Title, page.Summary, page.Lang, page.FetchedAt.Unix(),
			page.ContentHash, boolToInt(page.Promoted), existingID,
		); err != nil {
			return false, fmt.Errorf("searchindex: update lite page: %w", err)
		}
		return true, insertLiteFTS(ctx, tx, existingID, page, frequencies)
	case errors.Is(err, sql.ErrNoRows):
		result, execErr := tx.ExecContext(ctx, `
			INSERT INTO pages_lite(url, root, title, summary, lang, fetched_at, content_hash, promoted)
			VALUES(?,?,?,?,?,?,?,?)`,
			page.URL, page.Root, page.Title, page.Summary, page.Lang,
			page.FetchedAt.Unix(), page.ContentHash, boolToInt(page.Promoted),
		)
		if execErr != nil {
			return false, fmt.Errorf("searchindex: insert lite page: %w", execErr)
		}
		id, idErr := result.LastInsertId()
		if idErr != nil {
			return false, fmt.Errorf("searchindex: lite page id: %w", idErr)
		}
		return true, insertLiteFTS(ctx, tx, id, page, frequencies)
	default:
		return false, fmt.Errorf("searchindex: lookup lite page: %w", err)
	}
}

func insertLiteFTS(ctx context.Context, tx *sql.Tx, id int64, page LitePage, frequencies DocumentFrequencies) error {
	sketch, err := BuildSketch(page.Lang, page.Title, page.Headings, page.Body, frequencies)
	if err != nil {
		return err
	}
	title, err := indexText(page.Lang, page.Title)
	if err != nil {
		return err
	}
	headings, err := indexText(page.Lang, page.Headings)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO `+liteFTSTable(page.Lang)+`(rowid, title, headings, salient_hi, salient_lo) VALUES(?,?,?,?,?)`,
		id, title, headings, sketch.SalientHi, sketch.SalientLo,
	); err != nil {
		return fmt.Errorf("searchindex: insert lite fts: %w", err)
	}
	return nil
}

func liteFTSTable(lang string) string {
	if lang == LangChinese {
		return "lite_fts_zh"
	}
	return "lite_fts_en"
}

// liteColumnWeights order bm25 arguments as (title, headings, salient_hi,
// salient_lo). Title carries the page's own name for the intent, headings its
// structure, the hi sketch its distinctive vocabulary, the lo sketch the long
// tail; the weights only have to preserve that gradient — RRF fuses ranks,
// not scores, so cross-tier calibration is not required here.
var liteColumnWeights = []float64{8.0, 4.0, 2.0, 1.0}

// QueryLite ranks the discovery tier for one language. Promoted rows — pages
// whose body already lives in the full-text core — are excluded unless asked
// for, because the core lane already represents them.
func (s *Store) QueryLite(ctx context.Context, lang, text string, limit int, includePromoted bool) ([]LiteHit, error) {
	if err := s.guard(ctx); err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" || limit < 1 {
		return nil, nil
	}
	rewritten, err := queryText(lang, text)
	if err != nil {
		return nil, err
	}
	match := renderLiteMatch(rewritten)
	if match == "" {
		return nil, nil
	}
	table := liteFTSTable(lang)
	where := fmt.Sprintf("%s MATCH ?", table)
	if !includePromoted {
		where += " AND p.promoted = 0"
	}
	query := fmt.Sprintf(`
		SELECT p.url, p.title, p.root, p.summary, p.fetched_at,
		       bm25(%s, %f, %f, %f, %f) AS score
		FROM %s JOIN pages_lite p ON p.id = %s.rowid
		WHERE %s ORDER BY score, p.url LIMIT ?`,
		table, liteColumnWeights[0], liteColumnWeights[1], liteColumnWeights[2], liteColumnWeights[3],
		table, table, where)
	rows, err := s.db.QueryContext(ctx, query, match, limit)
	if err != nil {
		return nil, fmt.Errorf("searchindex: query %s: %w", table, err)
	}
	defer rows.Close()
	hits := make([]LiteHit, 0, limit)
	for rows.Next() {
		var hit LiteHit
		if err := rows.Scan(&hit.URL, &hit.Title, &hit.Root, &hit.Summary, &hit.FetchedAt, &hit.Score); err != nil {
			return nil, fmt.Errorf("searchindex: scan lite hit: %w", err)
		}
		hit.Lang = lang
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

// renderLiteMatch builds an OR query over quoted tokens: discovery recall is
// deliberately loose, and column weighting plus rank fusion do the judging.
// Quoting removes every FTS5 operator meaning from user text.
func renderLiteMatch(rewritten string) string {
	fields := strings.Fields(rewritten)
	if len(fields) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.ReplaceAll(field, `"`, "")
		if field == "" {
			continue
		}
		quoted = append(quoted, `"`+field+`"`)
	}
	return strings.Join(quoted, " OR ")
}

// storedFrequencies serves DF lookups from the df_stats table through a
// bounded in-memory cache; the backfill touches most of the vocabulary, so
// the cache keeps the second and later documents cheap.
type storedFrequencies struct {
	store     *Store
	lang      string
	documents int64
	cache     map[string]int64
}

func (f *storedFrequencies) Documents() int64 { return f.documents }

func (f *storedFrequencies) DocumentFrequency(term string) int64 {
	if cached, ok := f.cache[term]; ok {
		return cached
	}
	var df int64
	err := f.store.db.QueryRow(`SELECT df FROM df_stats WHERE lang=? AND term=?`, f.lang, term).Scan(&df)
	if err != nil {
		df = 0
	}
	if len(f.cache) < 2_000_000 {
		f.cache[term] = df
	}
	return df
}

// perLanguageFrequencies routes sketch lookups by the page's language.
type perLanguageFrequencies struct {
	english DocumentFrequencies
	chinese DocumentFrequencies
	lang    string
}

func (p *perLanguageFrequencies) Documents() int64 {
	if p.lang == LangChinese {
		return p.chinese.Documents()
	}
	return p.english.Documents()
}

func (p *perLanguageFrequencies) DocumentFrequency(term string) int64 {
	if p.lang == LangChinese {
		return p.chinese.DocumentFrequency(term)
	}
	return p.english.DocumentFrequency(term)
}

// LoadDocumentFrequencies opens DF lookups for both languages; a corpus with
// no df_meta rows yet answers zero everywhere, which BuildSketch tolerates
// (every term is equally rare, term frequency decides).
func (s *Store) LoadDocumentFrequencies(ctx context.Context) (map[string]DocumentFrequencies, error) {
	result := map[string]DocumentFrequencies{}
	for _, lang := range []string{LangEnglish, LangChinese} {
		var documents int64
		err := s.db.QueryRowContext(ctx, `SELECT documents FROM df_meta WHERE lang=?`, lang).Scan(&documents)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("searchindex: read df meta: %w", err)
		}
		result[lang] = &storedFrequencies{store: s, lang: lang, documents: documents, cache: map[string]int64{}}
	}
	return result, nil
}

// frequenciesFor adapts the per-language map to one page's language.
func frequenciesFor(frequencies map[string]DocumentFrequencies, lang string) DocumentFrequencies {
	return &perLanguageFrequencies{english: frequencies[LangEnglish], chinese: frequencies[LangChinese], lang: lang}
}

// BuildDFStats replaces the document-frequency table from the full-text
// corpus in one pass: every stored body contributes its unique terms once.
// It is the prerequisite for any sketching.
func (s *Store) BuildDFStats(ctx context.Context, progress func(done int)) (int, error) {
	if err := s.guard(ctx); err != nil {
		return 0, err
	}
	s.gate.RLock()
	defer s.gate.RUnlock()
	if s.closed {
		return 0, ErrClosed
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	counts := map[string]map[string]int64{LangEnglish: {}, LangChinese: {}}
	documents := map[string]int64{}
	rows, err := s.db.QueryContext(ctx, `SELECT body_z, lang FROM pages`)
	if err != nil {
		return 0, fmt.Errorf("searchindex: scan bodies: %w", err)
	}
	done := 0
	for rows.Next() {
		var blob []byte
		var lang string
		if err := rows.Scan(&blob, &lang); err != nil {
			_ = rows.Close()
			return 0, err
		}
		body, err := decompressBody(blob)
		if err != nil {
			_ = rows.Close()
			return 0, err
		}
		tokens, err := sketchTokens(lang, body)
		if err != nil {
			_ = rows.Close()
			return 0, err
		}
		seen := map[string]struct{}{}
		table := counts[LangEnglish]
		if lang == LangChinese {
			table = counts[LangChinese]
		}
		for _, token := range tokens {
			if _, duplicate := seen[token]; duplicate {
				continue
			}
			seen[token] = struct{}{}
			table[token]++
		}
		documents[lang]++
		done++
		if progress != nil && done%5000 == 0 {
			progress(done)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("searchindex: begin df write: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM df_stats`); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM df_meta`); err != nil {
		return 0, err
	}
	insert, err := tx.PrepareContext(ctx, `INSERT INTO df_stats(lang, term, df) VALUES(?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer insert.Close()
	for lang, table := range counts {
		for term, df := range table {
			if _, err := insert.ExecContext(ctx, lang, term, df); err != nil {
				return 0, err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO df_meta(lang, documents) VALUES(?,?)`, lang, documents[lang]); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("searchindex: commit df stats: %w", err)
	}
	return done, nil
}

// BackfillLite creates a discovery-tier row for every full-text page, marked
// promoted because their bodies already live in the core. This gives fusion a
// single uniform discovery layer and gives the sketcher its first real
// corpus without fetching a single page.
func (s *Store) BackfillLite(ctx context.Context, progress func(done int)) (int, error) {
	if err := s.guard(ctx); err != nil {
		return 0, err
	}
	frequencies, err := s.LoadDocumentFrequencies(ctx)
	if err != nil {
		return 0, err
	}
	type source struct {
		url, root, title, lead, lang, hash string
		fetchedAt                          int64
		body                               string
	}
	// Snapshot identifiers first, then page through bodies, so the backfill
	// does not hold one enormous read transaction against the writer.
	rows, err := s.db.QueryContext(ctx, `SELECT url, root, title, lead, body_z, lang, fetched_at, content_hash FROM pages ORDER BY id`)
	if err != nil {
		return 0, err
	}
	batch := make([]source, 0, liteBatchSize)
	done := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		pages := make([]LitePage, 0, len(batch))
		for _, item := range batch {
			pages = append(pages, LitePage{
				URL: item.url, Root: item.root, Title: item.title, Summary: item.lead,
				Body: item.body, Lang: item.lang, FetchedAt: time.Unix(item.fetchedAt, 0).UTC(),
				ContentHash: item.hash, Promoted: true,
			})
		}
		if _, err := s.upsertLiteBatch(ctx, pages, frequencies); err != nil {
			return err
		}
		done += len(batch)
		if progress != nil {
			progress(done)
		}
		batch = batch[:0]
		return nil
	}
	for rows.Next() {
		var item source
		var blob []byte
		if err := rows.Scan(&item.url, &item.root, &item.title, &item.lead, &blob, &item.lang, &item.fetchedAt, &item.hash); err != nil {
			_ = rows.Close()
			return done, err
		}
		body, err := decompressBody(blob)
		if err != nil {
			_ = rows.Close()
			return done, err
		}
		item.body = body
		batch = append(batch, item)
		if len(batch) >= liteBatchSize {
			if err := flush(); err != nil {
				_ = rows.Close()
				return done, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return done, err
	}
	_ = rows.Close()
	return done, flush()
}

// upsertLiteBatch is UpsertLiteMany with the frequency table already loaded,
// so the backfill does not reload it per batch.
func (s *Store) upsertLiteBatch(ctx context.Context, pages []LitePage, frequencies map[string]DocumentFrequencies) (int, error) {
	s.gate.RLock()
	defer s.gate.RUnlock()
	if s.closed {
		return 0, ErrClosed
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("searchindex: begin lite batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	written := 0
	for _, page := range pages {
		changed, err := upsertLiteTx(ctx, tx, page, frequenciesFor(frequencies, page.Lang))
		if err != nil {
			return 0, err
		}
		if changed {
			written++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("searchindex: commit lite batch: %w", err)
	}
	return written, nil
}
