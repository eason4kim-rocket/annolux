package main

import (
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eason4kim-rocket/annolux/internal/searcheval"
)

type v2ReleaseGate struct {
	SchemaVersion     int    `json:"schema_version"`
	Name              string `json:"name"`
	FrozenAt          string `json:"frozen_at"`
	CorpusName        string `json:"corpus_name"`
	Split             string `json:"split"`
	ExpectedCases     int    `json:"expected_cases"`
	RankingLockSHA256 string `json:"ranking_lock_sha256"`
	Minimum           struct {
		HitAt3  float64 `json:"hit_at_3"`
		HitAt10 float64 `json:"hit_at_10"`
		MRRAt10 float64 `json:"mrr_at_10"`
	} `json:"minimum"`
	Maximum struct {
		Errors             int   `json:"errors"`
		ClientP95LatencyMS int64 `json:"client_p95_latency_ms"`
	} `json:"maximum"`
	FailurePolicy string `json:"failure_policy"`
}

type v2GateResult struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	RunID         string `json:"run_id"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at"`
	Artifact      struct {
		SHA256 string `json:"sha256"`
		Lines  int    `json:"lines"`
	} `json:"artifact"`
	Identity struct {
		RankingLockSHA256 string `json:"ranking_lock_sha256"`
		CorpusSHA256      string `json:"corpus_sha256"`
		ManifestSHA256    string `json:"manifest_sha256"`
		Index             struct {
			Pages           int64 `json:"pages"`
			EnglishPages    int64 `json:"english_pages"`
			ChinesePages    int64 `json:"chinese_pages"`
			LatestFetchedAt int64 `json:"latest_fetched_at"`
			DatabaseBytes   int64 `json:"database_bytes"`
			WALBytes        int64 `json:"wal_bytes"`
		} `json:"index"`
	} `json:"identity"`
	Observed struct {
		Total                  int     `json:"total"`
		Errors                 int     `json:"errors"`
		Strong                 int     `json:"strong"`
		Acceptable             int     `json:"acceptable"`
		Weak                   int     `json:"weak"`
		Miss                   int     `json:"miss"`
		HitAt1                 float64 `json:"hit_at_1"`
		HitAt3                 float64 `json:"hit_at_3"`
		HitAt10                float64 `json:"hit_at_10"`
		MRRAt10                float64 `json:"mrr_at_10"`
		MeanNDCG               float64 `json:"mean_ndcg"`
		ClientMedianLatencyMS  int64   `json:"client_median_latency_ms"`
		ClientP95LatencyMS     int64   `json:"client_p95_latency_ms"`
		ClientMaximumLatencyMS int64   `json:"client_maximum_latency_ms"`
		DuplicateURLs          int     `json:"duplicate_urls"`
		SourceTwinHits         int     `json:"source_twin_hits"`
		ResponseDeduplicated   int     `json:"response_deduplicated"`
	} `json:"observed"`
	Verdict                 string `json:"verdict"`
	PerCaseResultsInspected bool   `json:"per_case_results_inspected"`
}

func TestFrozenCorpusV2PreservesAdjudicatedDev(t *testing.T) {
	v1, err := searcheval.LoadCorpus("corpus/v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := searcheval.LoadCorpus("corpus/v2/manifest.json")
	if err != nil {
		t.Fatal(err)
	}

	wantDev, err := v1.CasesForSplit("dev")
	if err != nil {
		t.Fatal(err)
	}
	indexByID := make(map[string]int, len(wantDev))
	for index, testCase := range wantDev {
		indexByID[testCase.ID] = index
	}
	for _, overlayPath := range []string{
		filepath.Join(round6AdjudicationDir, "dev-targets-v2-overlay.jsonl"),
		filepath.Join(round13AdjudicationDir, "dev-targets-v2-overlay.jsonl"),
	} {
		for _, overlay := range readStrictJSONLines[adjudicationOverlay](t, overlayPath) {
			index, ok := indexByID[overlay.ID]
			if !ok {
				t.Fatalf("overlay %q is not a v1 dev case", overlay.ID)
			}
			wantDev[index].Targets = overlay.Targets
		}
	}

	gotDev, err := v2.CasesForSplit("dev")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotDev, wantDev) {
		t.Fatal("v2 dev differs from v1 dev plus accepted adjudication overlays")
	}
}

func TestFrozenCorpusV2HeldoutIsFreshAndBlind(t *testing.T) {
	v1, err := searcheval.LoadCorpus("corpus/v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := searcheval.LoadCorpus("corpus/v2/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	heldout, err := v2.CasesForSplit("heldout")
	if err != nil {
		t.Fatal(err)
	}
	if len(heldout) != 40 {
		t.Fatalf("v2 heldout cases = %d, want 40", len(heldout))
	}

	v1IDs := make(map[string]struct{}, len(v1.Cases))
	v1Queries := make(map[string]struct{}, len(v1.Cases))
	v1Targets := make(map[string]struct{})
	for _, testCase := range v1.Cases {
		v1IDs[testCase.ID] = struct{}{}
		v1Queries[normalizedQuery(testCase.Query)] = struct{}{}
		for _, target := range testCase.Targets {
			v1Targets[canonicalEvaluationURL(t, target.URL)] = struct{}{}
		}
	}

	seenTargets := make(map[string]string, len(heldout))
	for _, testCase := range heldout {
		if !hasTag(testCase.Tags, "blind-v2") {
			t.Fatalf("heldout case %q is not marked blind-v2", testCase.ID)
		}
		if _, exists := v1IDs[testCase.ID]; exists {
			t.Fatalf("v2 heldout id %q occurs in v1", testCase.ID)
		}
		if _, exists := v1Queries[normalizedQuery(testCase.Query)]; exists {
			t.Fatalf("v2 heldout query %q occurs in v1", testCase.Query)
		}
		for _, target := range testCase.Targets {
			canonical := canonicalEvaluationURL(t, target.URL)
			if _, exists := v1Targets[canonical]; exists {
				t.Fatalf("v2 heldout target %q occurs in v1", target.URL)
			}
			if previous, exists := seenTargets[canonical]; exists {
				t.Fatalf("v2 heldout target %q is shared by %q and %q", target.URL, previous, testCase.ID)
			}
			seenTargets[canonical] = testCase.ID
		}
	}
}

func TestFrozenCorpusV2ReleaseGateWasDeclaredBeforeRun(t *testing.T) {
	corpus, err := searcheval.LoadCorpus("corpus/v2/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	gate := readStrictJSONFile[v2ReleaseGate](t, "corpus/v2/release-gate.json")
	if gate.SchemaVersion != 1 || gate.Name != "search-generalization-v2-heldout-gate" ||
		gate.FrozenAt != corpus.Manifest.FrozenAt || gate.CorpusName != corpus.Manifest.Name ||
		gate.Split != "heldout" || gate.ExpectedCases != corpus.Manifest.Expected.Splits["heldout"] ||
		gate.RankingLockSHA256 != "23c0d03f6c2a32057bcd7a2503a1047a249fde37b99edee3dc136db193255ed0" || strings.TrimSpace(gate.FailurePolicy) == "" {
		t.Fatalf("invalid v2 release gate identity: %#v", gate)
	}
	if gate.Minimum.HitAt3 != 0.65 || gate.Minimum.HitAt10 != 0.80 || gate.Minimum.MRRAt10 != 0.55 ||
		gate.Maximum.Errors != 0 || gate.Maximum.ClientP95LatencyMS != 1000 {
		t.Fatalf("v2 release thresholds changed: %#v", gate)
	}
}

func TestRecordedV2HeldoutResultPassesFrozenGate(t *testing.T) {
	corpus, err := searcheval.LoadCorpus("corpus/v2/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	gate := readStrictJSONFile[v2ReleaseGate](t, "corpus/v2/release-gate.json")
	result := readStrictJSONFile[v2GateResult](t, "corpus/v2/gate-result.json")

	if result.SchemaVersion != 1 || result.Name != "search-generalization-v2-heldout-gate-result" ||
		result.RunID != "v2-frozen-baseline" || result.StartedAt == "" || result.FinishedAt == "" ||
		result.Artifact.SHA256 != "d99840326f63dde34185c3410ace8e7e6ff7e728d451dc3bfe660a01fe765bd6" ||
		result.Artifact.Lines != gate.ExpectedCases+2 {
		t.Fatalf("invalid v2 gate-result artifact identity: %#v", result)
	}
	if result.Identity.RankingLockSHA256 != gate.RankingLockSHA256 ||
		result.Identity.CorpusSHA256 != corpus.Digest || result.Identity.ManifestSHA256 != corpus.ManifestDigest ||
		result.Identity.Index.Pages != 251367 || result.Identity.Index.EnglishPages != 215484 ||
		result.Identity.Index.ChinesePages != 35883 || result.Identity.Index.LatestFetchedAt != 1786747096 ||
		result.Identity.Index.DatabaseBytes != 2941472768 || result.Identity.Index.WALBytes != 0 {
		t.Fatalf("invalid v2 gate-result run identity: %#v", result.Identity)
	}
	passed := result.Observed.Total == gate.ExpectedCases &&
		result.Observed.Errors <= gate.Maximum.Errors &&
		result.Observed.HitAt3 >= gate.Minimum.HitAt3 &&
		result.Observed.HitAt10 >= gate.Minimum.HitAt10 &&
		result.Observed.MRRAt10 >= gate.Minimum.MRRAt10 &&
		result.Observed.ClientP95LatencyMS <= gate.Maximum.ClientP95LatencyMS
	if !passed || result.Verdict != "pass" || result.PerCaseResultsInspected {
		t.Fatalf("v2 result does not preserve a blind gate pass: %#v", result)
	}
	if result.Observed.Strong+result.Observed.Acceptable+result.Observed.Weak+result.Observed.Miss != result.Observed.Total {
		t.Fatalf("v2 verdict counts do not sum to total: %#v", result.Observed)
	}
}

func normalizedQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

func canonicalEvaluationURL(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		t.Fatalf("invalid target URL %q", raw)
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if path == "" {
		path = "/"
	}
	if parsed.RawQuery != "" {
		path += "?" + parsed.RawQuery
	}
	return host + path
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}
