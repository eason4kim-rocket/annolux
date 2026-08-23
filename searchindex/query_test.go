package searchindex

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestQueryPrefersMatchingLanguageAndMergesByRank(t *testing.T) {
	store := openTestStore(t)
	mustUpsert(t, store, Page{
		URL: "https://en.example/go", Root: "example", Title: "Go language",
		Body: "The Go programming language is compiled.", Lang: LangEnglish, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://zh.example/go", Root: "example", Title: "Go 语言",
		Body: "Go 编程语言是编译型的。", Lang: LangChinese, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://en.example/other", Root: "example", Title: "Other",
		Body: "unrelated english document about rivers", Lang: LangEnglish, FetchedAt: time.Now(),
	})

	hits, err := store.Query(context.Background(), "Go 编程语言", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Query returned no hits")
	}
	if hits[0].Lang != LangChinese || hits[0].URL != "https://zh.example/go" {
		t.Fatalf("preferred first hit = %#v", hits[0])
	}

	english, err := store.Query(context.Background(), "Go programming language", 5)
	if err != nil || len(english) == 0 || english[0].URL != "https://en.example/go" {
		t.Fatalf("english Query() = %#v, %v", english, err)
	}
}

// TestQueryMixedHanLatinUsesAllTermsWithoutProximityTrap locks the mixed-query
// tier choice. A product name may appear in the page header while the Chinese
// intent appears much later in the article, so requiring all tokens inside one
// NEAR window would let a shallow summary hide the canonical documentation.
func TestQueryMixedHanLatinUsesAllTermsWithoutProximityTrap(t *testing.T) {
	store := openTestStore(t)
	mustUpsert(t, store, Page{
		URL: "https://docs.python.example/tutorial/datastructures", Root: "python.example", Title: "5. 数据结构",
		Body: "Python " + strings.Repeat("导航章节参考内容 ", 12) + "列表推导式可以简洁地构造列表。",
		Lang: LangChinese, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://news.example/summary", Root: "news.example", Title: "Python 摘要",
		Body: "Python 列表推导式只是这篇摘要中的一句话。", Lang: LangChinese, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://news.example/python", Root: "news.example", Title: "Python 新闻",
		Body: "Python 发布了新版本。", Lang: LangChinese, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://tutorial.example/python3/tutorial", Root: "tutorial.example", Title: "Python3 教程",
		Body: "Python 列表推导式入门。", Lang: LangChinese, FetchedAt: time.Now(),
	})

	hits, err := store.Query(context.Background(), "python 列表推导式", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	foundCanonical := false
	for _, hit := range hits {
		if hit.URL == "https://docs.python.example/tutorial/datastructures" {
			foundCanonical = true
		}
		if hit.URL == "https://news.example/python" {
			t.Fatalf("single-term mixed-language noise leaked into AND results: %#v", hits)
		}
	}
	if !foundCanonical {
		t.Fatalf("canonical mixed-language page was trapped behind NEAR: %#v", hits)
	}
	if hits[0].URL != "https://docs.python.example/tutorial/datastructures" {
		t.Fatalf("canonical mixed-language page did not rank first: %#v", hits)
	}
}

func TestQueryCanonicalizesPostgresToPostgreSQL(t *testing.T) {
	store := openTestStore(t)
	mustUpsert(t, store, Page{
		URL: "https://postgres.example/manual", Root: "postgres.example", Title: "PostgreSQL manual",
		Body: "The PostgreSQL database server manual.", Lang: LangEnglish, FetchedAt: time.Now(),
	})
	hits, err := store.Query(context.Background(), "postgres", 5)
	if err != nil || len(hits) != 1 || hits[0].URL != "https://postgres.example/manual" {
		t.Fatalf("Postgres alias Query() = %#v, %v", hits, err)
	}
}

// TestQueryReranksBoundedCandidatesByTitleCoverage locks the small second-stage
// lexical ranker. Short release notes can dominate raw BM25, but an authority
// page whose title names more of the requested operation must survive a
// five-result provider window.
func TestQueryReranksBoundedCandidatesByTitleCoverage(t *testing.T) {
	store := openTestStore(t)
	canonicalURL := "https://postgres.example/docs/sql-createindex"
	mustUpsert(t, store, Page{
		URL: canonicalURL, Root: "postgres.example", Title: "CREATE INDEX",
		Body: "PostgreSQL supports CREATE INDEX CONCURRENTLY for production deployments. " +
			strings.Repeat("Detailed reference material and examples. ", 80),
		Lang: LangEnglish, FetchedAt: time.Now(),
	})
	for index := 0; index < 8; index++ {
		mustUpsert(t, store, Page{
			URL: fmt.Sprintf("https://news.example/release-%d", index), Root: "news.example",
			Title: fmt.Sprintf("PostgreSQL release %d", index),
			Body:  "PostgreSQL create index concurrently release note.",
			Lang:  LangEnglish, FetchedAt: time.Now(),
		})
	}

	hits, err := store.Query(context.Background(), "postgres create index concurrently", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) == 0 || hits[0].URL != canonicalURL {
		t.Fatalf("title-aware candidate ranking = %#v", hits)
	}
}

// TestQueryLimitDoesNotChangePromiseTopK reproduces the production Promise
// drift: the response size must only truncate one stable ranking, not change
// how many FTS candidates participate in lexical reranking.
func TestQueryLimitDoesNotChangePromiseTopK(t *testing.T) {
	store := openTestStore(t)
	canonicalURL := "https://javascript.example/advanced/promise"
	mustUpsert(t, store, Page{
		URL: canonicalURL, Root: "javascript.example", Title: "Promise 对象用法",
		Body: "JavaScript Promise 用法。" + strings.Repeat("详细文档解释异步操作与错误处理。", 120),
		Lang: LangChinese, FetchedAt: time.Now(),
	})
	for index := 0; index < 30; index++ {
		mustUpsert(t, store, Page{
			URL: fmt.Sprintf("https://news.example/release-%02d", index), Root: "news.example",
			Title: "JavaScript Promise 用法示例",
			Body:  "JavaScript Promise 用法。",
			Lang:  LangChinese, FetchedAt: time.Now(),
		})
	}

	assertStableQueryPrefixes(t, store, "javascript promise 用法", canonicalURL)
}

// TestQueryLimitKeepsStableTopKAcrossLanguages covers both tokenizer paths.
// The synthetic pages deliberately tie on every ranking signal except URL so
// the test also locks the deterministic final tie-break across response sizes.
func TestQueryLimitKeepsStableTopKAcrossLanguages(t *testing.T) {
	tests := []struct {
		name  string
		lang  string
		query string
		title string
		body  string
	}{
		{
			name:  "english",
			lang:  LangEnglish,
			query: "http status code",
			title: "HTTP status code reference",
			body:  "An HTTP status code explains the outcome of a request.",
		},
		{
			name:  "chinese",
			lang:  LangChinese,
			query: "缓存 配置",
			title: "缓存配置指南",
			body:  "这份缓存配置指南解释容量与超时设置。",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := openTestStore(t)
			for index := 0; index < 24; index++ {
				mustUpsert(t, store, Page{
					URL:       fmt.Sprintf("https://%s.example/manual-%02d", test.name, index),
					Root:      test.name + ".example",
					Title:     test.title,
					Body:      test.body,
					Lang:      test.lang,
					FetchedAt: time.Now(),
				})
			}

			assertStableQueryPrefixes(t, store, test.query, "")
		})
	}
}

// TestQueryPrefersScopedTutorialOverHistoricalTitle locks the bounded
// product-root expansion. Canonical documentation often omits the product
// name from its article title and body, while old release notes repeat it near
// every intent word. The root supplies the product scope; the tutorial path
// and intent match should then beat the historical page.
func TestQueryPrefersScopedTutorialOverHistoricalTitle(t *testing.T) {
	store := openTestStore(t)
	canonicalURL := "https://docs.python.example/tutorial/data-structures"
	mustUpsert(t, store, Page{
		URL: canonicalURL, Root: "python.example", Title: "5. Data Structures",
		Body: "Lists support a list comprehension for concise transformations. " +
			strings.Repeat("Detailed tutorial examples and explanations. ", 40),
		Lang: LangEnglish, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://docs.python.example/reference/expressions", Root: "python.example", Title: "6. Expressions",
		Body: "Python expressions include a list comprehension and generator expressions.",
		Lang: LangEnglish, FetchedAt: time.Now(),
	})
	for index := 0; index < 30; index++ {
		mustUpsert(t, store, Page{
			URL:  fmt.Sprintf("https://docs.python.example/whatsnew/release-%02d", index),
			Root: "python.example", Title: "What's New in Python",
			Body: "Python list comprehension release note.", Lang: LangEnglish, FetchedAt: time.Now(),
		})
	}

	assertStableQueryPrefixes(t, store, "python list comprehension", canonicalURL)
}

// TestQueryTreatsDocumentPurposeWordsAsOptionalRecall locks the global soft
// lane used by generic queries. The canonical status-code page does not need
// to repeat the user's explanatory word in order to enter the bounded window;
// its exact subject title still has to beat a framework-specific strict hit.
func TestQueryTreatsDocumentPurposeWordsAsOptionalRecall(t *testing.T) {
	store := openTestStore(t)
	canonicalURL := "https://www.runoob.example/http/http-status-codes.html"
	mustUpsert(t, store, Page{
		URL: canonicalURL, Root: "runoob.example", Title: "HTTP 状态码",
		Body: "HTTP 状态码用于说明请求结果。" + strings.Repeat("这里列出不同类别的响应。", 40),
		Lang: LangChinese, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://framework.example/status", Root: "framework.example", Title: "FastAPI 状态码含义",
		Body: "FastAPI 使用 HTTP 状态码表示响应含义。", Lang: LangChinese, FetchedAt: time.Now(),
	})

	hits, err := store.Query(context.Background(), "http 状态码 含义", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) == 0 || hits[0].URL != canonicalURL {
		t.Fatalf("soft document-purpose recall = %#v", hits)
	}
}

// TestQueryRecallsIntentFromIndexedPublisherRoot covers the product and
// publisher patterns behind the adjudicated dev misses. The target omits at
// least one strict query word, so it can only enter through the proven root;
// no query-specific target URL is encoded in production logic.
func TestQueryRecallsIntentFromIndexedPublisherRoot(t *testing.T) {
	tests := []struct {
		name, query, targetURL, root, title, body string
	}{
		{
			name: "python tutorial", query: "python errors exceptions tutorial",
			targetURL: "https://docs.python.org/tutorial/errors.html", root: "python.org",
			title: "8. Errors and Exceptions", body: "Errors and exceptions interrupt normal program execution.",
		},
		{
			name: "node introduction", query: "nodejs built in test runner introduction",
			targetURL: "https://nodejs.org/learn/test-runner/introduction", root: "nodejs.org",
			title: "Discovering Node.js's test runner", body: "The built in test runner executes test suites.",
		},
		{
			name: "php manual", query: "php language exceptions manual",
			targetURL: "https://www.php.net/manual/en/language.exceptions.php", root: "php.net",
			title: "PHP: Exceptions - Manual", body: "Exceptions provide structured error handling.",
		},
		{
			name: "mdn alias", query: "mdn css grid layout guide",
			targetURL: "https://developer.mozilla.org/en-US/docs/Web/CSS/Guides/Grid_layout", root: "mozilla.org",
			title: "CSS grid layout - CSS | MDN", body: "Grid layout arranges content in rows and columns.",
		},
		{
			name: "US government alias", query: "US government passport information",
			targetURL: "https://usa.gov/passport", root: "usa.gov",
			title: "U.S. passports | USAGov", body: "Learn how to apply for or renew a passport.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := openTestStore(t)
			mustUpsert(t, store, Page{
				URL: test.targetURL, Root: test.root, Title: test.title, Body: test.body,
				Lang: LangEnglish, FetchedAt: time.Now(),
			})
			for index := 0; index < 8; index++ {
				mustUpsert(t, store, Page{
					URL:  fmt.Sprintf("https://news.example/messages/%s-%02d", strings.ReplaceAll(test.name, " ", "-"), index),
					Root: "news.example", Title: test.query,
					Body: test.query, Lang: LangEnglish, FetchedAt: time.Now(),
				})
			}

			hits, err := store.Query(context.Background(), test.query, 10)
			if err != nil {
				t.Fatalf("Query() error = %v", err)
			}
			if rank := hitRank(hits, test.targetURL); rank < 1 || rank > 3 {
				t.Fatalf("scoped target rank = %d, hits = %#v", rank, hits)
			}
		})
	}
}

// TestQueryKeepsAllMatchingPublisherRoots prevents a root-selection shortcut
// from mistaking an ecosystem subproject for the canonical publisher. The
// strict candidates prove rust-embedded.org, while the body-only answer lives
// on rust-lang.org; bounded token-scoped recall must retain both roots.
func TestQueryKeepsAllMatchingPublisherRoots(t *testing.T) {
	store := openTestStore(t)
	targetURL := "https://doc.rust-lang.org/book/ch09-00-error-handling.html"
	mustUpsert(t, store, Page{
		URL: targetURL, Root: "rust-lang.org", Title: "Error Handling",
		Body: "Error handling uses Result and panic for recoverable and unrecoverable failures.",
		Lang: LangEnglish, FetchedAt: time.Now(),
	})
	for index := 0; index < 12; index++ {
		mustUpsert(t, store, Page{
			URL: fmt.Sprintf("https://docs.rust-embedded.org/messages/%02d", index), Root: "rust-embedded.org",
			Title: "The Embedded Rust Book", Body: "Rust error handling result panic.",
			Lang: LangEnglish, FetchedAt: time.Now(),
		})
	}

	hits, err := store.Query(context.Background(), "rust error handling result panic", 10)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) == 0 || hits[0].URL != targetURL {
		t.Fatalf("multi-root scoped recall = %#v", hits)
	}
}

// TestQueryDoesNotSkipBodyRecallAfterPlausibleTitles covers landing pages that
// omit one context word from their title. Seeing other complete-looking titles
// must not suppress the bounded body lane before the canonical page is found.
func TestQueryDoesNotSkipBodyRecallAfterPlausibleTitles(t *testing.T) {
	store := openTestStore(t)
	targetURL := "https://developer.mozilla.org/en-US/docs/Web/Accessibility"
	mustUpsert(t, store, Page{
		URL: targetURL, Root: "mozilla.org", Title: "Accessibility | MDN",
		Body: "Web accessibility helps people use websites and applications. " + strings.Repeat("Reference material. ", 200),
		Lang: LangEnglish, FetchedAt: time.Now(),
	})
	for index := 0; index < 24; index++ {
		mustUpsert(t, store, Page{
			URL:  fmt.Sprintf("https://blog.mozilla.org/web-accessibility-%02d", index),
			Root: "mozilla.org", Title: "Web accessibility article",
			Body: "Web accessibility article.", Lang: LangEnglish, FetchedAt: time.Now(),
		})
	}

	hits, err := store.Query(context.Background(), "mdn web accessibility guide", 10)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if rank := hitRank(hits, targetURL); rank < 1 || rank > 10 {
		t.Fatalf("landing page fell out after plausible titles: rank=%d hits=%#v", rank, hits)
	}
}

// TestQueryProjectsMixedLanguageTermsIntoEnglishRecall ensures Han-only intent
// cannot make an English official page fail FTS before root-scoped recall.
// The Chinese result may lead because language preference is explicit, but
// the official target must be relevant within the first three results.
func TestQueryProjectsMixedLanguageTermsIntoEnglishRecall(t *testing.T) {
	store := openTestStore(t)
	targetURL := "https://docs.docker.com/engine/storage/volumes/"
	mustUpsert(t, store, Page{
		URL: targetURL, Root: "docker.com", Title: "Volumes",
		Body: "Volumes provide persistent storage for containers.", Lang: LangEnglish, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://zh.example/kubernetes-volumes", Root: "example", Title: "Docker 数据卷 persistent storage",
		Body: "Docker 数据卷提供 persistent storage。", Lang: LangChinese, FetchedAt: time.Now(),
	})

	hits, err := store.Query(context.Background(), "Docker 数据卷 persistent storage", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if rank := hitRank(hits, targetURL); rank < 1 || rank > 3 {
		t.Fatalf("mixed-language official target rank = %d, hits = %#v", rank, hits)
	}
}

func TestQuerySkipsSingleBrandEnglishProbeForHanIntent(t *testing.T) {
	store := openTestStore(t)
	targetURL := "https://docs.python.example/zh-cn/tutorial/data-structures"
	mustUpsert(t, store, Page{
		URL: targetURL, Root: "python.example", Title: "Python 列表推导式",
		Body: "Python 列表推导式可以简洁地构造列表。", Lang: LangChinese, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://python.example/downloads", Root: "python.example", Title: "Python downloads",
		Body: "Download Python releases.", Lang: LangEnglish, FetchedAt: time.Now(),
	})

	hits, err := store.Query(context.Background(), "python 列表推导式", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) != 1 || hits[0].URL != targetURL {
		t.Fatalf("single-brand English probe leaked generic noise: %#v", hits)
	}
}

func TestQueryPrefersExactIntentLandingPath(t *testing.T) {
	store := openTestStore(t)
	targetURL := "https://usa.gov/passport"
	mustUpsert(t, store, Page{
		URL: targetURL, Root: "usa.gov", Title: "U.S. passports | USAGov",
		Body: "Find passport application and renewal information.", Lang: LangEnglish, FetchedAt: time.Now(),
	})
	for _, path := range []string{"renew-adult-passport", "child-passport", "apply-adult-passport", "lost-visa-passport"} {
		mustUpsert(t, store, Page{
			URL: "https://usa.gov/" + path, Root: "usa.gov", Title: strings.ReplaceAll(path, "-", " "),
			Body: "US government passport information for one specific task.", Lang: LangEnglish, FetchedAt: time.Now(),
		})
	}

	hits, err := store.Query(context.Background(), "US government passport information", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) == 0 || hits[0].URL != targetURL {
		t.Fatalf("exact intent landing path = %#v", hits)
	}
}

func TestPluralNormalizationIsTitleOnly(t *testing.T) {
	wanted := map[string]struct{}{"passport": {}, "guide": {}}
	if coverage := titleTermCoverage("U.S. passports", wanted); coverage != 1 {
		t.Fatalf("title plural coverage = %d, want 1", coverage)
	}
	if coverage := termCoverage("/Accessibility/Guides", wanted); coverage != 0 {
		t.Fatalf("URL plural coverage = %d, want 0", coverage)
	}
}

// TestQueryPrefersTopicLandingPageOverImagePages locks the URL-purpose prior
// used for two-token publisher queries such as MedlinePlus asthma.
func TestQueryPrefersTopicLandingPageOverImagePages(t *testing.T) {
	store := openTestStore(t)
	targetURL := "https://medlineplus.gov/asthma.html"
	mustUpsert(t, store, Page{
		URL: targetURL, Root: "medlineplus.gov", Title: "Asthma",
		Body: "Asthma is a chronic disease that affects the airways.", Lang: LangEnglish, FetchedAt: time.Now(),
	})
	for index := 0; index < 12; index++ {
		mustUpsert(t, store, Page{
			URL: fmt.Sprintf("https://medlineplus.gov/ency/imagepages/%04d.htm", index), Root: "medlineplus.gov",
			Title: "Asthma image", Body: "An asthma medical image.", Lang: LangEnglish, FetchedAt: time.Now(),
		})
	}

	hits, err := store.Query(context.Background(), "MedlinePlus asthma", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) == 0 || hits[0].URL != targetURL {
		t.Fatalf("topic landing-page ranking = %#v", hits)
	}
}

// TestQueryUsesBoundedScopedTitleRecall keeps a long exact reference visible
// when more than one fixed window of short same-root pages dominates BM25.
func TestQueryUsesBoundedScopedTitleRecall(t *testing.T) {
	store := openTestStore(t)
	targetURL := "https://rfc-editor.org/info/rfc8446/"
	mustUpsert(t, store, Page{
		URL: targetURL, Root: "rfc-editor.org",
		Title: "RFC 8446: The Transport Layer Security (TLS) Protocol Version 1.3",
		Body:  "TLS protocol version 1.3. " + strings.Repeat("Detailed normative specification text. ", 500),
		Lang:  LangEnglish, FetchedAt: time.Now(),
	})
	for index := 0; index < lexicalCandidateWindow+8; index++ {
		mustUpsert(t, store, Page{
			URL: fmt.Sprintf("https://rfc-editor.org/messages/%03d", index), Root: "rfc-editor.org",
			Title: fmt.Sprintf("RFC message %d", index), Body: "RFC TLS protocol version 1.3.",
			Lang: LangEnglish, FetchedAt: time.Now(),
		})
	}

	hits, err := store.Query(context.Background(), "RFC TLS protocol version 1.3", 10)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) == 0 || hits[0].URL != targetURL {
		t.Fatalf("scoped title recall = %#v", hits)
	}
}

func hitRank(hits []Hit, targetURL string) int {
	for index, hit := range hits {
		if hit.URL == targetURL {
			return index + 1
		}
	}
	return 0
}

func assertStableQueryPrefixes(t *testing.T, store *Store, query, firstURL string) {
	t.Helper()
	limits := []int{5, 10, 20}
	var previous []Hit
	previousLimit := 0
	for _, limit := range limits {
		hits, err := store.Query(context.Background(), query, limit)
		if err != nil {
			t.Fatalf("Query(%q, limit=%d) error = %v", query, limit, err)
		}
		if len(hits) != limit {
			t.Fatalf("Query(%q, limit=%d) result size = %d", query, limit, len(hits))
		}
		if firstURL != "" && hits[0].URL != firstURL {
			t.Fatalf("Query(%q, limit=%d) first URL = %q, want %q", query, limit, hits[0].URL, firstURL)
		}
		for index := range previous {
			if previous[index].URL != hits[index].URL {
				t.Fatalf(
					"Query(%q) prefix differs at %d between limits %d and %d: %q != %q",
					query, index, previousLimit, limit, previous[index].URL, hits[index].URL,
				)
			}
		}
		previous = hits
		previousLimit = limit
	}
}

// TestQueryUsesSegmentedChineseTitleCoverage locks title scoring against the
// same token stream written to the Chinese FTS table. A generic HTTP reference
// names the complete request in its title; a framework-specific page only
// mentions HTTP in the body and must not outrank it on body density alone.
func TestQueryUsesSegmentedChineseTitleCoverage(t *testing.T) {
	store := openTestStore(t)
	canonicalURL := "https://zh.example/general-status"
	mustUpsert(t, store, Page{
		URL: canonicalURL, Root: "zh.example", Title: "HTTP 状态码",
		Body: "HTTP 状态码用于说明请求结果。" + strings.Repeat("这里列出不同类别的响应。", 40),
		Lang: LangChinese, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://zh.example/framework-status", Root: "zh.example", Title: "FastAPI 响应状态码",
		Body: "HTTP 状态码用于 FastAPI 响应。", Lang: LangChinese, FetchedAt: time.Now(),
	})

	hits, err := store.Query(context.Background(), "http 状态码", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) == 0 || hits[0].URL != canonicalURL {
		t.Fatalf("Chinese title-aware ranking = %#v", hits)
	}
}

// TestQueryUsesURLIntentCoverage locks the URL-path signal. Documentation
// sites repeat their brand in every title, so BM25 and title coverage tie on a
// generic query; the page living under the requested scheduler topic should
// beat an I/O subsystem page.
func TestQueryUsesURLIntentCoverage(t *testing.T) {
	store := openTestStore(t)
	canonicalURL := "https://docs.kernel.example/scheduler/cfs"
	mustUpsert(t, store, Page{
		URL: canonicalURL, Root: "kernel.example", Title: "CFS Scheduler — The Linux Kernel documentation",
		Body: "The Linux kernel scheduler uses CFS for ordinary processes. " +
			strings.Repeat("Detailed design description. ", 40),
		Lang: LangEnglish, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://docs.kernel.example/block/kyber", Root: "kernel.example", Title: "Kyber I/O Scheduler — The Linux Kernel documentation",
		Body: "Linux kernel scheduler tunables.", Lang: LangEnglish, FetchedAt: time.Now(),
	})

	hits, err := store.Query(context.Background(), "linux kernel scheduler", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) == 0 || hits[0].URL != canonicalURL {
		t.Fatalf("URL intent-aware ranking = %#v", hits)
	}
}

// TestQueryPrefersStableDocumentationOverPrereleaseMirror locks the release
// channel prior. Exact-content mirrors are collapsed after provider ranking,
// so the stable URL must lead before a beta or nightly copy can become the
// surviving representative.
func TestQueryPrefersStableDocumentationOverPrereleaseMirror(t *testing.T) {
	store := openTestStore(t)
	stableURL := "https://doc.rust.example/book/ownership"
	for _, pageURL := range []string{
		"https://doc.rust.example/beta/book/ownership",
		stableURL,
	} {
		mustUpsert(t, store, Page{
			URL: pageURL, Root: "rust.example", Title: "Understanding Ownership - The Rust Programming Language",
			Body: "Rust ownership includes borrowing and references.", Lang: LangEnglish, FetchedAt: time.Now(),
		})
	}

	hits, err := store.Query(context.Background(), "rust ownership borrowing", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) == 0 || hits[0].URL != stableURL {
		t.Fatalf("stable documentation ranking = %#v", hits)
	}
}

func TestQueryEscapesFTSOperators(t *testing.T) {
	store := openTestStore(t)
	mustUpsert(t, store, Page{
		URL: "https://example.com/and", Root: "example.com", Title: "AND OR",
		Body: "token about apples", Lang: LangEnglish, FetchedAt: time.Now(),
	})
	hits, err := store.Query(context.Background(), `AND OR "apples"`, 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) != 1 || hits[0].URL != "https://example.com/and" {
		t.Fatalf("escaped query hits = %#v", hits)
	}
}

// TestQueryProximitySuppressesHubPages locks the NEAR tier: a link-directory
// page mentions every term somewhere in its soup, but only the content page
// holds them inside one passage, so the hub must stay out of the results.
func TestQueryProximitySuppressesHubPages(t *testing.T) {
	store := openTestStore(t)
	filler := strings.Repeat("unrelated filler words about many other subjects entirely ", 12)
	mustUpsert(t, store, Page{
		URL: "https://docs.example/status", Root: "docs.example", Title: "HTTP status codes explained",
		Body:      "Every http response carries a status code that tells the client what happened.",
		Lang:      LangEnglish,
		FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://hub.example/sitemap", Root: "hub.example", Title: "All articles",
		Body:      "http client tutorial " + filler + " status " + filler + " code reference " + filler,
		Lang:      LangEnglish,
		FetchedAt: time.Now(),
	})

	hits, err := store.Query(context.Background(), "http status code", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) != 1 || hits[0].URL != "https://docs.example/status" {
		t.Fatalf("NEAR-tier hits = %#v", hits)
	}
}

// TestQueryPrefersPagesMatchingAllTerms locks the AND-first pass: while any
// page holds every term, pages matching only one common term stay out of the
// results entirely instead of leaking into the head of the ranking.
func TestQueryPrefersPagesMatchingAllTerms(t *testing.T) {
	store := openTestStore(t)
	mustUpsert(t, store, Page{
		URL: "https://k8s.example/restart", Root: "k8s.example", Title: "Kubernetes pod restart policy",
		Body:      "The restart policy for pods controls how kubernetes restarts containers.",
		Lang:      LangEnglish,
		FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://blog.example/privacy", Root: "blog.example", Title: "Site policy",
		Body: "Our privacy policy explains cookie retention.", Lang: LangEnglish, FetchedAt: time.Now(),
	})

	hits, err := store.Query(context.Background(), "kubernetes restart policy", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) != 1 || hits[0].URL != "https://k8s.example/restart" {
		t.Fatalf("AND-first hits = %#v", hits)
	}
}

// TestQueryAndPassSuppressesOtherLanguageNoise locks the global strictness
// rule: when the preferred language satisfies AND, the other language table
// must not interleave weak single-term matches into the merged ranking.
func TestQueryAndPassSuppressesOtherLanguageNoise(t *testing.T) {
	store := openTestStore(t)
	mustUpsert(t, store, Page{
		URL: "https://k8s.example/restart", Root: "k8s.example", Title: "Kubernetes pod restart policy",
		Body:      "The restart policy for pods controls how kubernetes restarts containers.",
		Lang:      LangEnglish,
		FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://zh.example/k8s", Root: "example", Title: "Kubernetes 集群入门",
		Body: "这一篇介绍 kubernetes 集群的部署。", Lang: LangChinese, FetchedAt: time.Now(),
	})

	hits, err := store.Query(context.Background(), "kubernetes restart policy", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	for _, hit := range hits {
		if hit.URL == "https://zh.example/k8s" {
			t.Fatalf("other-language partial match leaked into AND results: %#v", hits)
		}
	}
	if len(hits) != 1 || hits[0].URL != "https://k8s.example/restart" {
		t.Fatalf("AND-first hits = %#v", hits)
	}
}

// TestQueryKeepsPartialMatches locks the OR fallback. When no page holds every
// term, the query degrades to OR so bm25 can still rank partial matches
// instead of returning nothing.
func TestQueryKeepsPartialMatches(t *testing.T) {
	store := openTestStore(t)
	mustUpsert(t, store, Page{
		URL: "https://example.com/nginx", Root: "example.com", Title: "Nginx reverse proxy",
		Body:      "How to configure the nginx reverse proxy read timeout for upstream servers.",
		Lang:      LangEnglish,
		FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://example.com/rivers", Root: "example.com", Title: "Rivers",
		Body: "An unrelated document about rivers and boats.", Lang: LangEnglish, FetchedAt: time.Now(),
	})

	// Only "nginx" is present anywhere; under AND this returned zero hits.
	hits, err := store.Query(context.Background(), "nginx kubernetes helm", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(hits) != 1 || hits[0].URL != "https://example.com/nginx" {
		t.Fatalf("partial-match hits = %#v", hits)
	}

	// A page matching more terms must outrank one matching fewer.
	ranked, err := store.Query(context.Background(), "nginx proxy timeout rivers", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(ranked) != 2 || ranked[0].URL != "https://example.com/nginx" {
		t.Fatalf("bm25 partial ranking = %#v", ranked)
	}
}

// TestQueryMatchesTwoCharacterChinese locks the bigram rewrite. The trigram
// tokenizer needs three characters, so it silently dropped the two-character
// words that dominate real Chinese queries.
func TestQueryMatchesTwoCharacterChinese(t *testing.T) {
	store := openTestStore(t)
	mustUpsert(t, store, Page{
		URL: "https://zh.example/redis", Root: "example", Title: "Redis 配置指南",
		Body:      "这是一篇讲 Redis 配置和价格的中文文档，包含缓存超时设置。",
		Lang:      LangChinese,
		FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://zh.example/other", Root: "example", Title: "无关页面",
		Body: "这一页讲别的东西，完全无关。", Lang: LangChinese, FetchedAt: time.Now(),
	})

	for _, query := range []string{"配置", "价格", "缓存"} {
		hits, err := store.Query(context.Background(), query, 5)
		if err != nil {
			t.Fatalf("Query(%q) error = %v", query, err)
		}
		if len(hits) == 0 || hits[0].URL != "https://zh.example/redis" {
			t.Fatalf("Query(%q) hits = %#v", query, hits)
		}
		// The snippet is cut from the stored body, so it must be original text.
		if hits[0].Snippet == "" || !strings.Contains(hits[0].Snippet, query) {
			t.Fatalf("Query(%q) snippet = %q", query, hits[0].Snippet)
		}
	}
}

// TestUpsertReplacesRewrittenChinesePostings locks delete/insert symmetry: an
// external-content delete replays indexed tokens, so a delete built from raw
// text would strand the bigram postings and keep serving the stale page.
func TestUpsertReplacesRewrittenChinesePostings(t *testing.T) {
	store := openTestStore(t)
	page := Page{
		URL: "https://zh.example/doc", Root: "example", Title: "旧标题",
		Body: "这里讲的是缓存策略。", Lang: LangChinese, FetchedAt: time.Now(),
	}
	mustUpsert(t, store, page)
	page.Title = "新标题"
	page.Body = "这里改成讲价格模型。"
	page.ContentHash = ""
	mustUpsert(t, store, page)

	stale, err := store.Query(context.Background(), "缓存", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("stale bigram postings survived: %#v", stale)
	}
	fresh, err := store.Query(context.Background(), "价格", 5)
	if err != nil || len(fresh) != 1 {
		t.Fatalf("refreshed Query() = %#v, %v", fresh, err)
	}
}

func mustUpsert(t *testing.T, store *Store, page Page) {
	t.Helper()
	if err := store.Upsert(context.Background(), page); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
}

// TestReindexRebuildsFromCompressedBodies locks the reason the compressed body
// is kept at all: a tokenizer change must cost one local pass, not a re-crawl.
func TestReindexRebuildsFromCompressedBodies(t *testing.T) {
	store := openTestStore(t)
	mustUpsert(t, store, Page{
		URL: "https://example.com/en", Root: "example.com", Title: "Nginx proxy",
		Body: "configure the nginx reverse proxy read timeout", Lang: LangEnglish, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://zh.example/cn", Root: "example", Title: "Redis 配置",
		Body: "这是讲 Redis 配置和价格的文档。", Lang: LangChinese, FetchedAt: time.Now(),
	})

	rebuilt, err := store.Reindex(context.Background())
	if err != nil {
		t.Fatalf("Reindex() error = %v", err)
	}
	if rebuilt != 2 {
		t.Fatalf("rebuilt = %d, want 2", rebuilt)
	}
	for _, query := range []string{"nginx timeout", "配置", "价格"} {
		hits, err := store.Query(context.Background(), query, 5)
		if err != nil || len(hits) == 0 {
			t.Fatalf("after reindex Query(%q) = %#v, %v", query, hits, err)
		}
		if hits[0].Snippet == "" {
			t.Fatalf("after reindex Query(%q) lost its snippet", query)
		}
	}
}

// TestQueryMatchesChineseCompoundParts locks what search-mode segmentation buys
// over character bigrams: a compound is indexed together with its parts, so a
// query for either half reaches the page without matching arbitrary character
// pairs that straddle two words.
func TestQueryMatchesChineseCompoundParts(t *testing.T) {
	store := openTestStore(t)
	mustUpsert(t, store, Page{
		URL: "https://zh.example/pricing", Root: "example", Title: "计费说明",
		Body: "价格按内存容量计费，缓存命中率越高单位成本越低。", Lang: LangChinese, FetchedAt: time.Now(),
	})
	mustUpsert(t, store, Page{
		URL: "https://zh.example/rivers", Root: "example", Title: "河流",
		Body: "这一页讲河流与航运，和计价无关。", Lang: LangChinese, FetchedAt: time.Now(),
	})

	for _, query := range []string{"内存容量", "内存", "容量", "命中率"} {
		hits, err := store.Query(context.Background(), query, 5)
		if err != nil {
			t.Fatalf("Query(%q) error = %v", query, err)
		}
		if len(hits) == 0 || hits[0].URL != "https://zh.example/pricing" {
			t.Fatalf("Query(%q) hits = %#v", query, hits)
		}
	}

	// "存容" spans the boundary of 内存|容量 and is not a word, so a word index
	// must not match it the way a character bigram index would.
	spurious, err := store.Query(context.Background(), "存容", 5)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(spurious) != 0 {
		t.Fatalf("cross-word fragment matched: %#v", spurious)
	}
}

// TestEnglishQueryNeverLoadsChineseDictionary locks the lazy-load claim. Every
// query probes both language tables, so without a Han short-circuit an
// English-only deployment would still pay the dictionary's ~130 MB on its very
// first query.
func TestEnglishQueryNeverLoadsChineseDictionary(t *testing.T) {
	if segmenterLoaded() {
		t.Skip("another test in this package already built the dictionary")
	}
	store := openTestStore(t)
	mustUpsert(t, store, Page{
		URL: "https://example.com/go", Root: "example.com", Title: "Go scheduler",
		Body: "the go scheduler multiplexes goroutines onto threads", Lang: LangEnglish, FetchedAt: time.Now(),
	})
	if _, err := store.Query(context.Background(), "goroutine scheduler threads", 5); err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if segmenterLoaded() {
		t.Fatal("an English-only query loaded the Chinese dictionary")
	}
}
