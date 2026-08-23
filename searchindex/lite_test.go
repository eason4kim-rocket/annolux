package searchindex

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fixedFrequencies struct {
	documents int64
	table     map[string]int64
}

func (f fixedFrequencies) Documents() int64 { return f.documents }
func (f fixedFrequencies) DocumentFrequency(term string) int64 {
	return f.table[term]
}

func TestBuildSketchRanksRareTermsFirstAndExcludesTitle(t *testing.T) {
	frequencies := fixedFrequencies{documents: 1000, table: map[string]int64{
		"kubernetes": 900, "the": 990, "autoscaler": 3, "hpa": 2, "replicas": 40,
		"scales": 500, "threshold": 300,
	}}
	body := strings.Repeat("the kubernetes autoscaler scales replicas ", 3) + "hpa threshold"
	sketch, err := BuildSketch(LangEnglish, "Kubernetes Autoscaling", "", body, frequencies)
	if err != nil {
		t.Fatal(err)
	}
	hi := strings.Fields(sketch.SalientHi)
	if len(hi) == 0 {
		t.Fatal("empty sketch")
	}
	joined := " " + sketch.SalientHi + " " + sketch.SalientLo + " "
	// Title terms never re-enter the sketch.
	for _, banned := range []string{" kubernetes ", " autoscaling "} {
		if strings.Contains(joined, banned) {
			t.Fatalf("title term leaked into sketch: %q", joined)
		}
	}
	// The rarest body terms outrank the common ones.
	if hi[0] != "autoscaler" && hi[0] != "hpa" {
		t.Fatalf("hi sketch head = %q, want a rare term (%q)", hi[0], sketch.SalientHi)
	}
	// Determinism.
	again, err := BuildSketch(LangEnglish, "Kubernetes Autoscaling", "", body, frequencies)
	if err != nil || again != sketch {
		t.Fatalf("sketch not deterministic: %+v vs %+v (%v)", sketch, again, err)
	}
}

func TestBuildSketchCapsAndSplits(t *testing.T) {
	terms := make([]string, 0, 300)
	for index := 0; index < 300; index++ {
		terms = append(terms, "term"+string(rune('a'+index%26))+strings.Repeat("x", index/26+1))
	}
	body := strings.Join(terms, " ")
	sketch, err := BuildSketch(LangEnglish, "", "", body, fixedFrequencies{documents: 10})
	if err != nil {
		t.Fatal(err)
	}
	hi, lo := strings.Fields(sketch.SalientHi), strings.Fields(sketch.SalientLo)
	if len(hi) != sketchHiTerms || len(hi)+len(lo) != sketchMaxTerms {
		t.Fatalf("sketch sizes hi=%d lo=%d", len(hi), len(lo))
	}
}

func TestBuildSketchChineseSegments(t *testing.T) {
	body := "内存容量决定系统吞吐。数据库索引优化依赖统计信息。"
	sketch, err := BuildSketch(LangChinese, "系统调优", "", body, fixedFrequencies{documents: 100})
	if err != nil {
		t.Fatal(err)
	}
	if sketch.SalientHi == "" {
		t.Fatal("Chinese sketch is empty")
	}
	if strings.Contains(" "+sketch.SalientHi+" "+sketch.SalientLo+" ", " 系统 ") {
		// 系统 comes from the title and must be excluded.
		t.Fatalf("title term leaked: %q", sketch.SalientHi+" "+sketch.SalientLo)
	}
}

func TestLiteUpsertQueryAndPromotionFlag(t *testing.T) {
	store := openLiteStore(t)
	fetched := time.Unix(1_786_900_000, 0).UTC()
	pages := []LitePage{
		{URL: "https://react.dev/reference/react/useState", Root: "react.dev", Title: "useState",
			Headings: "Usage Parameters", Summary: "useState is a React Hook that lets you add state.",
			Body: "useState hook returns stateful value and setter. Rerender component when state changes.",
			Lang: LangEnglish, FetchedAt: fetched},
		{URL: "https://vuejs.org/guide/essentials/reactivity.html", Root: "vuejs.org", Title: "Reactivity",
			Summary: "Reactive state fundamentals.", Body: "ref reactive computed watchers track dependencies",
			Lang: LangEnglish, FetchedAt: fetched},
		{URL: "https://example.cn/zh/优化", Root: "example.cn", Title: "性能优化",
			Summary: "数据库调优指南", Body: "数据库索引优化依赖统计信息与查询计划",
			Lang: LangChinese, FetchedAt: fetched},
		{URL: "https://promoted.example.com/full", Root: "example.com", Title: "Already Promoted",
			Summary: "has a full-text row", Body: "useState anything whatsoever", Lang: LangEnglish,
			FetchedAt: fetched, Promoted: true},
	}
	written, err := store.UpsertLiteMany(context.Background(), pages)
	if err != nil || written != 4 {
		t.Fatalf("upsert = %d, %v", written, err)
	}
	// Idempotent re-upsert changes nothing.
	written, err = store.UpsertLiteMany(context.Background(), pages)
	if err != nil || written != 0 {
		t.Fatalf("re-upsert = %d, %v", written, err)
	}

	hits, err := store.QueryLite(context.Background(), LangEnglish, "useState hook", 10, false)
	if err != nil || len(hits) == 0 {
		t.Fatalf("lite query = %d hits, %v", len(hits), err)
	}
	if hits[0].URL != "https://react.dev/reference/react/useState" {
		t.Fatalf("lite rank 1 = %s", hits[0].URL)
	}
	for _, hit := range hits {
		if hit.URL == "https://promoted.example.com/full" {
			t.Fatal("promoted row leaked into the discovery lane")
		}
		if hit.Summary == "" || hit.FetchedAt == 0 {
			t.Fatalf("hit missing serve fields: %+v", hit)
		}
	}
	including, err := store.QueryLite(context.Background(), LangEnglish, "useState hook", 10, true)
	if err != nil || len(including) <= len(hits) {
		t.Fatalf("includePromoted did not widen: %d vs %d, %v", len(including), len(hits), err)
	}
	zh, err := store.QueryLite(context.Background(), LangChinese, "数据库 优化", 10, false)
	if err != nil || len(zh) != 1 || zh[0].Lang != LangChinese {
		t.Fatalf("zh lite query = %+v, %v", zh, err)
	}
	// FTS5 operator characters in user text must be inert.
	if _, err := store.QueryLite(context.Background(), LangEnglish, `useState" OR NEAR( -"`, 10, false); err != nil {
		t.Fatalf("operator injection: %v", err)
	}
}

func TestDFStatsAndBackfill(t *testing.T) {
	store := openLiteStore(t)
	fetched := time.Unix(1_786_900_000, 0).UTC()
	fullPages := []Page{
		{URL: "https://docs.example.com/autoscaler", Root: "example.com", Title: "Autoscaler",
			Body: "the autoscaler adjusts replicas based on utilization metrics", Lang: LangEnglish, FetchedAt: fetched},
		{URL: "https://docs.example.com/scheduler", Root: "example.com", Title: "Scheduler",
			Body: "the scheduler assigns pods to nodes based on constraints", Lang: LangEnglish, FetchedAt: fetched},
		{URL: "https://example.cn/zh/索引", Root: "example.cn", Title: "索引",
			Body: "数据库索引优化依赖统计信息", Lang: LangChinese, FetchedAt: fetched},
	}
	for _, page := range fullPages {
		if err := store.Upsert(context.Background(), page); err != nil {
			t.Fatal(err)
		}
	}
	documents, err := store.BuildDFStats(context.Background(), nil)
	if err != nil || documents != 3 {
		t.Fatalf("df stats = %d docs, %v", documents, err)
	}
	frequencies, err := store.LoadDocumentFrequencies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if frequencies[LangEnglish].Documents() != 2 || frequencies[LangChinese].Documents() != 1 {
		t.Fatalf("df meta: en=%d zh=%d", frequencies[LangEnglish].Documents(), frequencies[LangChinese].Documents())
	}
	// "based" appears in both English bodies; "autoscaler" in one.
	if df := frequencies[LangEnglish].DocumentFrequency("based"); df != 2 {
		t.Fatalf("df(based) = %d", df)
	}
	if df := frequencies[LangEnglish].DocumentFrequency("autoscaler"); df != 1 {
		t.Fatalf("df(autoscaler) = %d", df)
	}

	backfilled, err := store.BackfillLite(context.Background(), nil)
	if err != nil || backfilled != 3 {
		t.Fatalf("backfill = %d, %v", backfilled, err)
	}
	// Every lite row is promoted: the body already lives in the core, so the
	// discovery lane must stay silent until genuinely new pages arrive.
	hits, err := store.QueryLite(context.Background(), LangEnglish, "autoscaler replicas", 10, false)
	if err != nil || len(hits) != 0 {
		t.Fatalf("discovery lane not silent after backfill: %d, %v", len(hits), err)
	}
	promotedHits, err := store.QueryLite(context.Background(), LangEnglish, "autoscaler replicas", 10, true)
	if err != nil || len(promotedHits) == 0 {
		t.Fatalf("promoted lane empty: %v", err)
	}
	// Backfill twice stays idempotent.
	backfilled, err = store.BackfillLite(context.Background(), nil)
	if err != nil || backfilled != 3 {
		t.Fatalf("re-backfill = %d, %v", backfilled, err)
	}
}

func openLiteStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.TempDir() + "/index.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestQueryLiteRanksByRelevanceNotURL pins the regression where the FTS
// option pair contentless_delete + detail=column made bm25() return zero for
// every row, silently collapsing lane order to the URL tiebreak. The corpus
// is built so the relevant page sorts LAST by URL: alphabetical order and
// relevance order disagree, and only real scoring passes.
func TestQueryLiteRanksByRelevanceNotURL(t *testing.T) {
	store := openLiteStore(t)
	fetched := time.Unix(1_786_900_000, 0).UTC()
	pages := []LitePage{
		{URL: "https://aaa.example.com/misc", Root: "aaa.example.com", Title: "Miscellany",
			Summary: "assorted notes", Body: "grid appears once among many unrelated diary words today",
			Lang: LangEnglish, FetchedAt: fetched},
		{URL: "https://bbb.example.com/notes", Root: "bbb.example.com", Title: "Notes",
			Summary: "more notes", Body: "layout mentioned in passing beside cooking recipes",
			Lang: LangEnglish, FetchedAt: fetched},
		{URL: "https://zzz.example.com/css-grid", Root: "zzz.example.com", Title: "CSS Grid Layout",
			Headings: "grid tracks template areas", Summary: "two dimensional layout",
			Body: "css grid layout defines rows columns tracks gaps fractions",
			Lang: LangEnglish, FetchedAt: fetched},
	}
	if written, err := store.UpsertLiteMany(context.Background(), pages); err != nil || written != 3 {
		t.Fatalf("upsert = %d, %v", written, err)
	}
	hits, err := store.QueryLite(context.Background(), LangEnglish, "css grid layout", 10, false)
	if err != nil || len(hits) == 0 {
		t.Fatalf("lite query = %d hits, %v", len(hits), err)
	}
	if hits[0].URL != "https://zzz.example.com/css-grid" {
		t.Fatalf("lane ignored relevance: rank 1 = %s (URL-order collapse?)", hits[0].URL)
	}
	for _, hit := range hits {
		if hit.Score == 0 {
			t.Fatalf("bm25 returned zero for %s: scoring is dead", hit.URL)
		}
	}
}
