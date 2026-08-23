package searchindex

// The schema is pre-release: it is edited in place rather than migrated, so a
// tokenizer or layout change means rebuilding the index file.
//
// Measured on a real 86-page crawl, storing plain body text cost 149% of the
// text while the inverted index alone cost 39%. Pages therefore keep a
// zstd-compressed body plus a short plain lead, and the FTS tables are
// contentless. That also removes the external-content coupling: the indexed
// token stream no longer has to equal the stored text, which is what a
// rewritten (segmented) Chinese stream needs.
const schemaMigrationV1 = `
CREATE TABLE pages (
	id INTEGER PRIMARY KEY,
	url TEXT NOT NULL UNIQUE,
	root TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	lead TEXT NOT NULL DEFAULT '',
	body_z BLOB,
	lang TEXT NOT NULL,
	fetched_at INTEGER NOT NULL,
	content_hash TEXT NOT NULL,
	etag TEXT NOT NULL DEFAULT '',
	last_mod TEXT NOT NULL DEFAULT '',
	needs_render INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX pages_root ON pages(root);
CREATE INDEX pages_hash ON pages(content_hash);
CREATE INDEX pages_render ON pages(needs_render) WHERE needs_render = 1;

CREATE VIRTUAL TABLE pages_fts_en USING fts5(
	title, body, content='', contentless_delete=1, tokenize='unicode61');
CREATE VIRTUAL TABLE pages_fts_zh USING fts5(
	title, body, content='', contentless_delete=1, tokenize='unicode61');

CREATE TABLE frontier (
	url TEXT PRIMARY KEY,
	root TEXT NOT NULL,
	state TEXT NOT NULL,
	leased_at INTEGER NOT NULL DEFAULT 0,
	attempts INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX frontier_pick ON frontier(state, root);
`

// In-crawl link discovery enforces a per-root row budget, which needs a count
// by root alone; frontier_pick leads on state and cannot serve that seek.
const schemaMigrationV2 = `
CREATE INDEX frontier_root ON frontier(root);
`

// Exact-duplicate URLs no longer earn their own body and FTS postings: one
// canonical page keeps the content, every other address of the same
// content_hash becomes an alias row. Aliases keep their own fetch metadata so
// a future recrawl of the address can still answer conditional requests, but
// they are invisible to search, which serves canonical URLs only.
const schemaMigrationV3 = `
CREATE TABLE url_aliases (
	url TEXT PRIMARY KEY,
	page_id INTEGER NOT NULL,
	fetched_at INTEGER NOT NULL,
	etag TEXT NOT NULL DEFAULT '',
	last_mod TEXT NOT NULL DEFAULT ''
);
CREATE INDEX url_aliases_page ON url_aliases(page_id);
`

// The discovery tier stores what finding a page needs and nothing a page has
// not earned: title, headings, a short summary, and a lexical sketch of the
// body's most significant terms. Its FTS tables keep the default detail=full:
// combining contentless_delete with detail=column makes bm25() return zero
// for every row (observed on SQLite 3.45 and covered by a regression test),
// which silently collapses lane ordering to the tiebreak. Positions cost a
// little space but keep ranking real; phrase and NEAR queries still belong to
// the full-text core. df_stats carries the corpus document frequencies the
// sketch's significance scoring needs, per language because the tokenizers
// differ.
const schemaMigrationV4 = `
CREATE TABLE pages_lite (
	id INTEGER PRIMARY KEY,
	url TEXT NOT NULL UNIQUE,
	root TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	lang TEXT NOT NULL,
	fetched_at INTEGER NOT NULL,
	content_hash TEXT NOT NULL,
	promoted INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX pages_lite_hash ON pages_lite(content_hash);
CREATE INDEX pages_lite_root ON pages_lite(root);

CREATE VIRTUAL TABLE lite_fts_en USING fts5(
	title, headings, salient_hi, salient_lo,
	content='', contentless_delete=1, tokenize='unicode61');
CREATE VIRTUAL TABLE lite_fts_zh USING fts5(
	title, headings, salient_hi, salient_lo,
	content='', contentless_delete=1, tokenize='unicode61');

CREATE TABLE df_stats (
	lang TEXT NOT NULL,
	term TEXT NOT NULL,
	df INTEGER NOT NULL,
	PRIMARY KEY (lang, term)
) WITHOUT ROWID;
CREATE TABLE df_meta (
	lang TEXT PRIMARY KEY,
	documents INTEGER NOT NULL
);
`

var migrations = []string{schemaMigrationV1, schemaMigrationV2, schemaMigrationV3, schemaMigrationV4}
