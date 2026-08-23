package searchindex

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

const (
	// snippetLeadRunes is the context kept before the first term hit;
	// snippetWindowRunes is the fragment finally returned.
	snippetLeadRunes   = 20
	snippetWindowRunes = 120

	// Multi-term strict queries rerank one fixed, bounded FTS candidate window
	// by title, URL, and document-purpose signals. It must not scale with the
	// response limit: changing page size may truncate this ranking, never change
	// which documents participated in it. Forty preserves the former candidate
	// set for the default limit of 10, fixes the observed 5/10 drift, and avoids
	// making every default cold query pay the former limit-20 worst case.
	lexicalCandidateWindow   = 40
	lexicalTitleRecallWindow = 20

	// These deliberately small integer weights keep BM25 as the final
	// tie-breaker while correcting three corpus-size pathologies: a product word
	// in a historical title, a framework page that only names the intent in its
	// body, and a generic documentation footer shared by every page on a site.
	titleCoverageWeight   = 8
	urlCoverageWeight     = 6
	documentUtilityWeight = 3
)

// Hit is one ranked page from a single FTS table.
type Hit struct {
	URL       string
	Title     string
	Root      string
	FetchedAt int64
	Score     float64
	Snippet   string
	Lang      string
}

// Query searches both language indexes, takes the top limit from each, and
// merges by rank position. BM25 scores are never compared across tokenizers.
// The query language is preferred at each rank.
func (s *Store) Query(ctx context.Context, text string, limit int) ([]Hit, error) {
	if err := s.guard(ctx); err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	if limit < 1 {
		limit = 1
	}
	terms := queryTerms(text)
	if len(terms) == 0 {
		return nil, nil
	}
	preferred := DetectLanguage(text)
	other := LangEnglish
	if preferred == LangEnglish {
		other = LangChinese
	}
	// Strictness is chosen globally, not per table: the strictest tier that
	// yields any hit is applied to both languages. A per-table fallback would
	// interleave weak single-term matches from the other language in between
	// strong full matches from the preferred one. Mixed Han/Latin queries are
	// the exception: a product name often lives in page chrome while the Han
	// intent lives in the article passage, so NEAR would discard the canonical
	// page before BM25 can rank it. AND still requires every term and therefore
	// does not admit the single-term noise that OR would.
	tiers := []matchTier{tierNear, tierAnd, tierOr}
	if mixedHanLatin(text) {
		tiers = []matchTier{tierAnd, tierOr}
	}
	for _, tier := range tiers {
		preferredHits, err := s.queryTable(ctx, preferred, terms, limit, tier)
		if err != nil {
			return nil, err
		}
		otherHits, err := s.queryTable(ctx, other, terms, limit, tier)
		if err != nil {
			return nil, err
		}
		if len(preferredHits)+len(otherHits) > 0 {
			return mergeHitsByRank(preferredHits, otherHits, limit), nil
		}
	}
	return nil, nil
}

// matchTier is one strictness level of the three-tier query plan.
type matchTier int

const (
	// tierNear requires every term within a small token window. Hub pages —
	// pagination indexes, link directories, sidebar soups — contain almost
	// every term somewhere, but only genuine content pages hold them close
	// together, so this tier keeps hubs out of the head of the ranking.
	tierNear matchTier = iota
	tierAnd
	tierOr
)

type rankedHit struct {
	hit          Hit
	lexicalScore int
	strict       bool
}

// nearWindow is the token distance allowed between query terms in tierNear.
const nearWindow = 20

func (s *Store) queryTable(ctx context.Context, lang string, terms []string, limit int, tier matchTier) ([]Hit, error) {
	match, tokens, err := ftsMatch(lang, terms, tier)
	if err != nil {
		return nil, err
	}
	if match == "" {
		return nil, nil
	}
	// A single Latin brand word from an otherwise Han query is not enough
	// English intent to justify a corpus-wide probe. The Chinese table already
	// carries the complete request; probing English for only "python" or "http"
	// adds generic noise and was the dominant mixed-query latency regression.
	if lang == LangEnglish && len(tokens) < 2 && termsContainHan(terms) {
		return nil, nil
	}
	// OR candidates do not all satisfy the same intent, so title coverage must
	// not let a one-term title jump ahead of a body matching several terms.
	lexicalRerank := len(tokens) > 1 && tier != tierOr
	candidateLimit := limit
	if lexicalRerank {
		candidateLimit = lexicalCandidateWindow
	}
	candidates, err := s.queryCandidates(ctx, lang, match, terms, tokens, candidateLimit, "", true, lexicalRerank)
	if err != nil {
		return nil, err
	}

	// A product or publisher scope is often carried by the host rather than the
	// article text. Resolve that scope against indexed roots, then recall the
	// remaining intent on that root. Generic document-purpose words stay ranking
	// signals but are optional for recall. If one crowded root fills the regular
	// window, a smaller title-only lane keeps an exact long-form reference from
	// being buried by short messages or image pages.
	rootToken, consumedScopeTerms, hasScope := queryScope(tokens)
	scopeAvailable := false
	if lexicalRerank && lang == LangEnglish && hasScope {
		scopeAvailable = hasScopeRootCandidate(candidates, rootToken)
		if !scopeAvailable {
			scopeAvailable, err = s.scopeRootExists(ctx, rootToken)
			if err != nil {
				return nil, err
			}
		}
	}
	if scopeAvailable {
		intentTokens := scopeRecallTokens(lang, tokens[consumedScopeTerms:])
		intentMatch := renderFTSMatch(intentTokens, tierAnd)
		if intentMatch != "" {
			scoped, queryErr := s.queryCandidates(
				ctx, lang, intentMatch, terms, tokens, candidateLimit, rootToken, false, true,
			)
			if queryErr != nil {
				return nil, queryErr
			}
			candidates = mergeRankedHits(candidates, scoped)
			if len(scoped) >= candidateLimit {
				titleMatch := renderTitleFTSMatch(intentTokens)
				titleCandidates, titleErr := s.queryCandidates(
					ctx, lang, titleMatch, terms, tokens, lexicalTitleRecallWindow, rootToken, false, true,
				)
				if titleErr != nil {
					return nil, titleErr
				}
				candidates = mergeRankedHits(candidates, titleCandidates)
			}
		}
	} else if lexicalRerank {
		softTokens := softRecallTokens(lang, tokens)
		if len(softTokens) < len(tokens) {
			softMatch := renderTitleFTSMatch(softTokens)
			softCandidates, queryErr := s.queryCandidates(
				ctx, lang, softMatch, terms, tokens, lexicalTitleRecallWindow, "", false, true,
			)
			if queryErr != nil {
				return nil, queryErr
			}
			candidates = mergeRankedHits(candidates, softCandidates)
		}
	}

	if lexicalRerank {
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].lexicalScore != candidates[j].lexicalScore {
				return candidates[i].lexicalScore > candidates[j].lexicalScore
			}
			if candidates[i].strict != candidates[j].strict {
				return candidates[i].strict
			}
			if candidates[i].hit.Score != candidates[j].hit.Score {
				return candidates[i].hit.Score < candidates[j].hit.Score
			}
			return candidates[i].hit.URL < candidates[j].hit.URL
		})
	}
	hits := make([]Hit, 0, min(limit, len(candidates)))
	for _, candidate := range candidates {
		hits = append(hits, candidate.hit)
		if len(hits) >= limit {
			break
		}
	}
	return hits, nil
}

func (s *Store) queryCandidates(
	ctx context.Context,
	lang, match string,
	snippetTerms, rankingTokens []string,
	limit int,
	rootToken string,
	strict, lexicalRerank bool,
) ([]rankedHit, error) {
	table := ftsTable(lang)
	// The FTS tables are contentless, so snippet() has no text to work from.
	// Fragments come from the stored plain lead instead, which also keeps the
	// compressed body off the query path entirely.
	where := fmt.Sprintf("%s MATCH ?", table)
	arguments := []any{match}
	if rootToken != "" {
		where += " AND instr(lower(p.root), ?) > 0"
		arguments = append(arguments, strings.ToLower(rootToken))
	}
	arguments = append(arguments, limit)
	query := fmt.Sprintf(`
		SELECT p.url, p.title, p.root, p.fetched_at, p.lead,
		       bm25(%s, 5.0, 1.0) AS score
		FROM %s JOIN pages p ON p.id = %s.rowid
		WHERE %s ORDER BY score, p.url LIMIT ?`, table, table, table, where)
	rows, err := s.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("searchindex: query %s: %w", table, err)
	}
	defer rows.Close()
	candidates := make([]rankedHit, 0, limit)
	for rows.Next() {
		var hit Hit
		var lead string
		if err := rows.Scan(&hit.URL, &hit.Title, &hit.Root, &hit.FetchedAt, &lead, &hit.Score); err != nil {
			return nil, fmt.Errorf("searchindex: scan hit: %w", err)
		}
		hit.Lang = lang
		hit.Snippet = plainSnippet(lead, snippetTerms)
		if rootToken != "" && !rootContainsQueryToken(hit.Root, rootToken) {
			continue
		}
		candidate := rankedHit{hit: hit, strict: strict}
		if lexicalRerank {
			candidate.lexicalScore, err = lexicalCandidateScore(lang, hit.Title, hit.URL, rankingTokens)
			if err != nil {
				return nil, err
			}
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func mergeHitsByRank(preferred, other []Hit, limit int) []Hit {
	merged := make([]Hit, 0, limit)
	seen := make(map[string]struct{}, limit)
	add := func(hit Hit) bool {
		if _, exists := seen[hit.URL]; exists {
			return len(merged) >= limit
		}
		seen[hit.URL] = struct{}{}
		merged = append(merged, hit)
		return len(merged) >= limit
	}
	maxRank := len(preferred)
	if len(other) > maxRank {
		maxRank = len(other)
	}
	for rank := 0; rank < maxRank; rank++ {
		if rank < len(preferred) && add(preferred[rank]) {
			return merged
		}
		if rank < len(other) && add(other[rank]) {
			return merged
		}
	}
	return merged
}

// queryTerms splits raw user text into candidate terms. Every FTS5 operator
// character is either a separator or ends up inside a quoted phrase, so user
// text can never reach the parser as syntax.
func queryTerms(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	})
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.ReplaceAll(field, `"`, "")
		if field == "" {
			continue
		}
		terms = append(terms, field)
	}
	return terms
}

// ftsMatch renders terms for one tier. Query walks NEAR, then AND, then OR:
// pure OR let a single common term ("policy", "变量") pull unrelated hub pages
// into the head of the ranking, pure AND still admitted link-directory pages
// that mention every term somewhere, and NEAR alone would return nothing for
// broad queries whose terms never sit in one passage.
func ftsMatch(lang string, terms []string, tier matchTier) (string, []string, error) {
	phrases, err := ftsTokens(lang, terms)
	if err != nil {
		return "", nil, err
	}
	return renderFTSMatch(phrases, tier), phrases, nil
}

func ftsTokens(lang string, terms []string) ([]string, error) {
	phrases := make([]string, 0, len(terms))
	seen := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = projectQueryTerm(lang, term)
		if term == "" {
			continue
		}
		// A Chinese term is segmented the same way the index was, so each word
		// becomes its own OR operand instead of one long phrase and a page does
		// not have to contain the whole run verbatim.
		segmented, err := queryText(lang, term)
		if err != nil {
			return nil, err
		}
		for _, token := range strings.Fields(segmented) {
			token = strings.ReplaceAll(token, `"`, "")
			if token == "" {
				continue
			}
			token = canonicalQueryToken(token)
			key := strings.ToLower(token)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			phrases = append(phrases, token)
		}
	}
	return phrases, nil
}

func projectQueryTerm(lang, term string) string {
	if lang != LangEnglish {
		return term
	}
	var projected strings.Builder
	for _, character := range term {
		if unicode.Is(unicode.Han, character) {
			projected.WriteByte(' ')
			continue
		}
		projected.WriteRune(character)
	}
	return strings.TrimSpace(projected.String())
}

func renderFTSMatch(tokens []string, tier matchTier) string {
	quoted := make([]string, len(tokens))
	for index, token := range tokens {
		quoted[index] = `"` + token + `"`
	}
	switch {
	case len(tokens) == 0:
		return ""
	case tier == tierOr:
		return strings.Join(quoted, " OR ")
	case tier == tierNear && len(tokens) > 1:
		return fmt.Sprintf("NEAR(%s, %d)", strings.Join(quoted, " "), nearWindow)
	default:
		// A single term makes NEAR meaningless, so it degrades to AND here
		// and Query's AND pass then finds nothing new to add.
		return strings.Join(quoted, " AND ")
	}
}

func renderTitleFTSMatch(tokens []string) string {
	clauses := make([]string, len(tokens))
	for index, token := range tokens {
		clauses[index] = `title : "` + token + `"`
	}
	return strings.Join(clauses, " AND ")
}

// softRecallTokens removes words that describe the kind of document the user
// wants rather than its subject. They remain in rankingTokens, so an exact
// title or URL still wins after the broader, bounded recall lane finds it.
func softRecallTokens(lang string, tokens []string) []string {
	soft := make(map[string]struct{})
	if lang == LangChinese {
		for _, token := range []string{"含义", "指南", "教程", "介绍", "概述", "信息"} {
			soft[token] = struct{}{}
		}
	} else {
		for _, token := range []string{
			"guide", "guides", "information", "intro", "introduction", "introductions",
			"manual", "manuals", "meaning", "overview", "overviews", "tutorial", "tutorials",
		} {
			soft[token] = struct{}{}
		}
	}
	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if _, optional := soft[strings.ToLower(token)]; !optional {
			filtered = append(filtered, token)
		}
	}
	if len(filtered) == 0 {
		return tokens
	}
	return filtered
}

func scopeRecallTokens(lang string, tokens []string) []string {
	filtered := softRecallTokens(lang, tokens)
	if lang != LangEnglish {
		return filtered
	}
	withoutQualifiers := make([]string, 0, len(filtered))
	for _, token := range filtered {
		switch strings.ToLower(token) {
		case "current", "language", "latest":
			continue
		default:
			withoutQualifiers = append(withoutQualifiers, token)
		}
	}
	if len(withoutQualifiers) == 0 {
		return filtered
	}
	return withoutQualifiers
}

// canonicalQueryToken applies only high-confidence lexical identities that
// the unicode61 index cannot infer. It deliberately does not attempt generic
// stemming: rewriting an unstemmed query to a guessed root would lose exact
// matches unless that root was also written into the index.
func canonicalQueryToken(token string) string {
	switch strings.ToLower(token) {
	case "postgres":
		return "postgresql"
	default:
		return token
	}
}

func mixedHanLatin(text string) bool {
	hasHan := false
	hasLatin := false
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			hasHan = true
		case unicode.Is(unicode.Latin, r):
			hasLatin = true
		}
		if hasHan && hasLatin {
			return true
		}
	}
	return false
}

func termsContainHan(terms []string) bool {
	for _, term := range terms {
		for _, character := range term {
			if unicode.Is(unicode.Han, character) {
				return true
			}
		}
	}
	return false
}

func lexicalCandidateScore(lang, title, rawURL string, tokens []string) (int, error) {
	wanted := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		wanted[strings.ToLower(canonicalQueryToken(token))] = struct{}{}
	}
	titleWanted := wanted
	urlWanted := wanted
	// Once the host already proves the leading product scope, repeating that
	// product in a title is not evidence that the page answers the intent. This
	// keeps generic hubs such as "Python Tutorial" from beating the exact
	// article while preserving title evidence for topical terms such as HTTP.
	// Known publisher aliases (MDN -> mozilla.org and US government -> usa.gov)
	// use the resolved root only for URL coverage; their query wording is not
	// required to be repeated in an article title.
	rootToken, consumedScopeTerms, hasScope := queryScope(tokens)
	explicitAlias := hasScope && (consumedScopeTerms > 1 || !strings.EqualFold(rootToken, canonicalQueryToken(tokens[0])))
	rootMatches := hasScope && urlHostContainsQueryToken(rawURL, rootToken)
	if explicitAlias || rootMatches {
		titleWanted = make(map[string]struct{}, len(wanted))
		urlWanted = make(map[string]struct{}, len(wanted))
		for token := range wanted {
			consumed := false
			for _, scopeTerm := range tokens[:consumedScopeTerms] {
				if token == strings.ToLower(canonicalQueryToken(scopeTerm)) {
					consumed = true
					break
				}
			}
			if !consumed {
				titleWanted[token] = struct{}{}
				urlWanted[token] = struct{}{}
			}
		}
	}
	if rootMatches {
		urlWanted[strings.ToLower(canonicalQueryToken(rootToken))] = struct{}{}
	}
	indexedTitle, err := indexText(lang, title)
	if err != nil {
		return 0, err
	}
	titleCoverage := titleTermCoverage(indexedTitle, titleWanted)
	urlCoverage, utility := urlLexicalFeatures(rawURL, urlWanted)
	return titleCoverage*titleCoverageWeight +
		urlCoverage*urlCoverageWeight +
		utility*documentUtilityWeight, nil
}

func urlHostContainsQueryToken(rawURL, token string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && rootContainsQueryToken(parsed.Hostname(), token)
}

func termCoverage(text string, wanted map[string]struct{}) int {
	matched := make(map[string]struct{}, len(wanted))
	for _, token := range queryTerms(text) {
		key := strings.ToLower(canonicalQueryToken(token))
		if _, exists := wanted[key]; !exists {
			continue
		}
		matched[key] = struct{}{}
	}
	return len(matched)
}

func titleTermCoverage(text string, wanted map[string]struct{}) int {
	normalizedWanted := make(map[string]struct{}, len(wanted))
	for token := range wanted {
		normalizedWanted[lexicalCoverageToken(token)] = struct{}{}
	}
	matched := make(map[string]struct{}, len(normalizedWanted))
	for _, token := range queryTerms(text) {
		key := lexicalCoverageToken(token)
		if _, exists := normalizedWanted[key]; exists {
			matched[key] = struct{}{}
		}
	}
	return len(matched)
}

// lexicalCoverageToken applies conservative English plural normalization only
// to title scoring. FTS recall and URL scoring remain exact and therefore
// cannot be broadened by this heuristic.
func lexicalCoverageToken(token string) string {
	token = strings.ToLower(canonicalQueryToken(token))
	switch {
	case len(token) > 4 && strings.HasSuffix(token, "ies"):
		return strings.TrimSuffix(token, "ies") + "y"
	case len(token) > 3 && strings.HasSuffix(token, "s") &&
		!strings.HasSuffix(token, "ss") && !strings.HasSuffix(token, "us") && !strings.HasSuffix(token, "is"):
		return strings.TrimSuffix(token, "s")
	default:
		return token
	}
}

func urlLexicalFeatures(rawURL string, wanted map[string]struct{}) (int, int) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0, 0
	}
	host := strings.ToLower(parsed.Hostname())
	coverage := termCoverage(host+" "+parsed.Path, wanted)
	utility := 0
	if label, _, _ := strings.Cut(host, "."); label != "" {
		switch label {
		case "doc", "docs", "developer":
			utility++
		case "archive", "archives", "blog", "blogs", "bugs", "issues", "news":
			utility -= 2
		}
	}
	positivePath, negativePath := 0, 0
	pathTerms := queryTerms(strings.ToLower(parsed.Path))
	exactIntentPath := len(pathTerms) > 0
	matchedIntentPath := 0
	for _, token := range pathTerms {
		if _, matchesIntent := wanted[strings.ToLower(canonicalQueryToken(token))]; matchesIntent {
			matchedIntentPath++
		} else {
			exactIntentPath = false
		}
		switch token {
		case "book", "books", "manual", "manuals", "tutorial", "tutorials":
			positivePath = max(positivePath, 3)
		case "doc", "docs", "documentation", "guide", "guides", "howto", "howtos", "reference", "references":
			positivePath = max(positivePath, 2)
		case "beta", "nightly", "preview", "unstable":
			negativePath = min(negativePath, -2)
		case "archive", "archives", "blog", "blogs", "bugs", "changelog", "changelogs", "image", "images", "imagepage", "imagepages", "issues", "message", "messages", "news", "release", "releases", "whatsnew":
			negativePath = min(negativePath, -3)
		}
	}
	if exactIntentPath && matchedIntentPath > 0 {
		utility++
	}
	return coverage, utility + positivePath + negativePath
}

// queryScope recognizes only a leading publisher/product scope. Most scopes
// use the literal first token; the aliases below are deliberately narrow and
// map user-facing publisher names to the normalized roots already in pages.
func queryScope(tokens []string) (rootToken string, consumed int, ok bool) {
	if len(tokens) < 2 {
		return "", 0, false
	}
	if strings.EqualFold(tokens[0], "mdn") {
		return "mozilla", 1, true
	}
	if len(tokens) >= 3 && strings.EqualFold(tokens[0], "US") && strings.EqualFold(tokens[1], "government") {
		return "usa", 2, true
	}
	if len(tokens) >= 4 && strings.EqualFold(tokens[0], "U") && strings.EqualFold(tokens[1], "S") && strings.EqualFold(tokens[2], "government") {
		return "usa", 3, true
	}
	rootToken = strings.ToLower(canonicalQueryToken(tokens[0]))
	if !scopeTokenSuitable(rootToken) {
		return "", 0, false
	}
	return rootToken, 1, true
}

// scopeRootExists proves a proposed scope through the indexed pages_root
// B-tree. The prefix range bounds the work; exact token matching below rejects
// lookalikes such as pythonic.example for a Python query.
func (s *Store) scopeRootExists(ctx context.Context, token string) (bool, error) {
	if !scopeTokenSuitable(token) {
		return false, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT root FROM pages
		WHERE root >= ? AND root < ? ORDER BY root LIMIT 16`, token, token+string(unicode.MaxRune))
	if err != nil {
		return false, fmt.Errorf("searchindex: resolve scope root: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var root string
		if err := rows.Scan(&root); err != nil {
			return false, fmt.Errorf("searchindex: scan scope root: %w", err)
		}
		if rootContainsQueryToken(root, token) {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("searchindex: resolve scope root: %w", err)
	}
	return false, nil
}

func hasScopeRootCandidate(candidates []rankedHit, token string) bool {
	if !scopeTokenSuitable(token) {
		return false
	}
	for _, candidate := range candidates {
		if rootContainsQueryToken(candidate.hit.Root, token) {
			return true
		}
	}
	return false
}

func scopeTokenSuitable(token string) bool {
	token = strings.ToLower(token)
	if len(token) < 3 {
		return false
	}
	for _, character := range token {
		if character > unicode.MaxASCII || !unicode.IsLetter(character) && !unicode.IsNumber(character) {
			return false
		}
	}
	return true
}

func rootContainsQueryToken(root, wanted string) bool {
	wanted = strings.ToLower(canonicalQueryToken(wanted))
	for _, token := range queryTerms(root) {
		if strings.ToLower(canonicalQueryToken(token)) == wanted {
			return true
		}
	}
	return false
}

func mergeRankedHits(primary, additional []rankedHit) []rankedHit {
	merged := make([]rankedHit, 0, len(primary)+len(additional))
	seen := make(map[string]struct{}, len(primary)+len(additional))
	for _, candidates := range [][]rankedHit{primary, additional} {
		for _, candidate := range candidates {
			if _, duplicate := seen[candidate.hit.URL]; duplicate {
				continue
			}
			seen[candidate.hit.URL] = struct{}{}
			merged = append(merged, candidate)
		}
	}
	return merged
}

// plainSnippet cuts a window around the first term hit in the stored body.
func plainSnippet(body string, terms []string) string {
	body = strings.Join(strings.Fields(body), " ")
	if body == "" {
		return ""
	}
	runes := []rune(body)
	start := 0
	for _, term := range terms {
		if at := strings.Index(body, term); at >= 0 {
			start = len([]rune(body[:at]))
			break
		}
	}
	start -= snippetLeadRunes
	if start < 0 {
		start = 0
	}
	end := start + snippetWindowRunes
	if end > len(runes) {
		end = len(runes)
	}
	fragment := string(runes[start:end])
	if end < len(runes) {
		fragment += "…"
	}
	if start > 0 {
		fragment = "…" + fragment
	}
	return fragment
}
