package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eason4kim-rocket/annolux/internal/searcheval"
)

const (
	round6AdjudicationDir  = "adjudication/round6-dev"
	round13AdjudicationDir = "adjudication/round13-dev"
)

type adjudicationManifest struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	AdjudicatedAt string `json:"adjudicated_at"`
	Source        struct {
		CorpusName      string `json:"corpus_name"`
		CorpusSHA256    string `json:"corpus_sha256"`
		RoundID         string `json:"round_id"`
		RoundSHA256     string `json:"round_sha256"`
		DeployedCommit  string `json:"deployed_commit"`
		EvaluationLimit int    `json:"evaluation_limit"`
	} `json:"source"`
	Scope struct {
		Split                  string `json:"split"`
		Misses                 int    `json:"misses"`
		HeldoutExcluded        int    `json:"heldout_excluded"`
		HeldoutAlreadyRevealed bool   `json:"heldout_already_revealed"`
	} `json:"scope"`
	Counts map[string]int    `json:"counts"`
	Files  map[string]string `json:"files"`
}

type adjudicationTarget struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Indexed bool   `json:"indexed"`
}

type adjudicationObserved struct {
	RankAt10         int    `json:"rank_at_10"`
	URL              string `json:"url"`
	Title            string `json:"title"`
	ContentHashEqual *bool  `json:"content_hash_equal,omitempty"`
}

type adjudicationDiagnostic struct {
	TargetRankAt20 int `json:"target_rank_at_20"`
}

type adjudicationJudgment struct {
	SchemaVersion   int                    `json:"schema_version"`
	CaseID          string                 `json:"case_id"`
	Query           string                 `json:"query"`
	Classification  string                 `json:"classification"`
	Target          adjudicationTarget     `json:"target"`
	Observed        adjudicationObserved   `json:"observed"`
	Diagnostic      adjudicationDiagnostic `json:"diagnostic"`
	TargetAdditions []searcheval.Target    `json:"target_additions"`
	Rationale       string                 `json:"rationale"`
}

type adjudicationOverlay struct {
	ID      string              `json:"id"`
	Targets []searcheval.Target `json:"targets"`
}

type adjudicationRankingError struct {
	CaseID         string `json:"case_id"`
	TargetURL      string `json:"target_url"`
	TargetRankAt10 int    `json:"target_rank_at_10"`
	TargetRankAt20 int    `json:"target_rank_at_20"`
	NextCheck      string `json:"next_check"`
}

type adjudicationTrueGaps struct {
	SchemaVersion int      `json:"schema_version"`
	SourceRound   string   `json:"source_round"`
	Split         string   `json:"split"`
	Cases         []string `json:"cases"`
	Reason        string   `json:"reason"`
}

type followupAliasJudgment struct {
	SchemaVersion  int    `json:"schema_version"`
	CaseID         string `json:"case_id"`
	Query          string `json:"query"`
	Classification string `json:"classification"`
	SourceRound    struct {
		ID     string `json:"id"`
		SHA256 string `json:"sha256"`
	} `json:"source_round"`
	Target struct {
		URL                 string `json:"url"`
		Title               string `json:"title"`
		ContentHash         string `json:"content_hash"`
		CompressedBodyBytes int    `json:"compressed_body_bytes"`
	} `json:"target"`
	Observed struct {
		Rank                int    `json:"rank"`
		URL                 string `json:"url"`
		Title               string `json:"title"`
		ContentHash         string `json:"content_hash"`
		CompressedBodyBytes int    `json:"compressed_body_bytes"`
	} `json:"observed"`
	TargetAdditions []searcheval.Target `json:"target_additions"`
	Rationale       string              `json:"rationale"`
}

func TestRound6DevAdjudicationIsCompleteAndHeldoutExcluded(t *testing.T) {
	corpus, err := searcheval.LoadCorpus("corpus/v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := make(map[string]searcheval.Case, len(corpus.Cases))
	for _, testCase := range corpus.Cases {
		cases[testCase.ID] = testCase
	}

	manifest := readStrictJSONFile[adjudicationManifest](t, filepath.Join(round6AdjudicationDir, "manifest.json"))
	if manifest.SchemaVersion != 1 || manifest.Name != "search-generalization-v1-round6-dev-adjudication" || manifest.AdjudicatedAt != "2026-08-15" ||
		manifest.Source.CorpusName != corpus.Manifest.Name || manifest.Source.CorpusSHA256 != corpus.Digest ||
		manifest.Source.RoundID != "round6" || manifest.Source.RoundSHA256 != "fffa428f7107a6c8f24716968e96da218cc52253ec945aecce60d165da487250" ||
		manifest.Source.DeployedCommit != "59abfdf651f5f7415597afa23949c463db084744" ||
		manifest.Source.EvaluationLimit != corpus.Manifest.Protocol.Limit {
		t.Fatalf("manifest source identity is invalid: %#v", manifest)
	}
	if manifest.Scope.Split != "dev" || manifest.Scope.Misses != 18 || manifest.Scope.HeldoutExcluded != 40 || !manifest.Scope.HeldoutAlreadyRevealed {
		t.Fatalf("manifest scope is invalid: %#v", manifest.Scope)
	}
	if len(manifest.Files) != 4 {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
	for _, name := range []string{"judgments.jsonl", "dev-targets-v2-overlay.jsonl", "ranking-errors.jsonl", "true-gaps.json"} {
		if manifest.Files[name] == "" {
			t.Fatalf("manifest does not bind %q", name)
		}
	}
	for name, wantDigest := range manifest.Files {
		contents, readErr := os.ReadFile(filepath.Join(round6AdjudicationDir, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		digest := sha256.Sum256(contents)
		if got := hex.EncodeToString(digest[:]); got != wantDigest {
			t.Fatalf("%s digest = %s, want %s", name, got, wantDigest)
		}
	}

	judgments := readStrictJSONLines[adjudicationJudgment](t, filepath.Join(round6AdjudicationDir, "judgments.jsonl"))
	if len(judgments) != manifest.Scope.Misses {
		t.Fatalf("judgments = %d, want %d", len(judgments), manifest.Scope.Misses)
	}
	judgmentByID := make(map[string]adjudicationJudgment, len(judgments))
	counts := map[string]int{"alias": 0, "better_answer": 0, "true_gap": 0, "ranking_error": 0}
	for _, judgment := range judgments {
		if judgment.SchemaVersion != 1 || strings.TrimSpace(judgment.Rationale) == "" || judgment.Observed.RankAt10 < 1 ||
			strings.TrimSpace(judgment.Target.Title) == "" || strings.TrimSpace(judgment.Observed.URL) == "" || strings.TrimSpace(judgment.Observed.Title) == "" ||
			judgment.Diagnostic.TargetRankAt20 < 0 || judgment.Diagnostic.TargetRankAt20 > 20 || !judgment.Target.Indexed {
			t.Fatalf("invalid judgment: %#v", judgment)
		}
		if _, duplicate := judgmentByID[judgment.CaseID]; duplicate {
			t.Fatalf("duplicate judgment %q", judgment.CaseID)
		}
		testCase, ok := cases[judgment.CaseID]
		if !ok || testCase.Split != "dev" || testCase.Query != judgment.Query || !containsTargetURL(testCase.Targets, judgment.Target.URL) {
			t.Fatalf("judgment is not bound to one dev case: %#v", judgment)
		}
		switch judgment.Classification {
		case "alias":
			if judgment.Observed.ContentHashEqual == nil || !*judgment.Observed.ContentHashEqual || len(judgment.TargetAdditions) == 0 {
				t.Fatalf("alias evidence is incomplete: %#v", judgment)
			}
		case "better_answer":
			if len(judgment.TargetAdditions) == 0 {
				t.Fatalf("better answer has no target addition: %#v", judgment)
			}
		case "true_gap", "ranking_error":
			if len(judgment.TargetAdditions) != 0 {
				t.Fatalf("%s unexpectedly changes targets: %#v", judgment.Classification, judgment)
			}
		default:
			t.Fatalf("unknown classification %q", judgment.Classification)
		}
		for _, target := range judgment.TargetAdditions {
			if strings.TrimSpace(target.URL) == "" || target.Relevance < 1 || target.Relevance > 3 || containsTargetURL(testCase.Targets, target.URL) {
				t.Fatalf("invalid target addition for %q: %#v", judgment.CaseID, target)
			}
		}
		judgmentByID[judgment.CaseID] = judgment
		counts[judgment.Classification]++
	}
	if !reflect.DeepEqual(counts, manifest.Counts) {
		t.Fatalf("classification counts = %#v, want %#v", counts, manifest.Counts)
	}

	overlays := readStrictJSONLines[adjudicationOverlay](t, filepath.Join(round6AdjudicationDir, "dev-targets-v2-overlay.jsonl"))
	if len(overlays) != counts["alias"]+counts["better_answer"] {
		t.Fatalf("overlays = %d", len(overlays))
	}
	seenOverlays := make(map[string]struct{}, len(overlays))
	for _, overlay := range overlays {
		if _, duplicate := seenOverlays[overlay.ID]; duplicate {
			t.Fatalf("duplicate overlay %q", overlay.ID)
		}
		judgment, ok := judgmentByID[overlay.ID]
		if !ok || (judgment.Classification != "alias" && judgment.Classification != "better_answer") {
			t.Fatalf("overlay without gold correction judgment: %#v", overlay)
		}
		wantTargets := append([]searcheval.Target(nil), cases[overlay.ID].Targets...)
		wantTargets = append(wantTargets, judgment.TargetAdditions...)
		if !reflect.DeepEqual(overlay.Targets, wantTargets) {
			t.Fatalf("overlay targets for %q = %#v, want %#v", overlay.ID, overlay.Targets, wantTargets)
		}
		seenOverlays[overlay.ID] = struct{}{}
	}

	rankingErrors := readStrictJSONLines[adjudicationRankingError](t, filepath.Join(round6AdjudicationDir, "ranking-errors.jsonl"))
	if len(rankingErrors) != counts["ranking_error"] {
		t.Fatalf("ranking errors = %d", len(rankingErrors))
	}
	seenRankingErrors := make(map[string]struct{}, len(rankingErrors))
	for _, rankingError := range rankingErrors {
		judgment, ok := judgmentByID[rankingError.CaseID]
		if !ok || judgment.Classification != "ranking_error" || rankingError.TargetURL != judgment.Target.URL ||
			rankingError.TargetRankAt10 != 0 || rankingError.TargetRankAt20 != judgment.Diagnostic.TargetRankAt20 || strings.TrimSpace(rankingError.NextCheck) == "" {
			t.Fatalf("invalid ranking error: %#v", rankingError)
		}
		if _, duplicate := seenRankingErrors[rankingError.CaseID]; duplicate {
			t.Fatalf("duplicate ranking error %q", rankingError.CaseID)
		}
		seenRankingErrors[rankingError.CaseID] = struct{}{}
	}

	trueGaps := readStrictJSONFile[adjudicationTrueGaps](t, filepath.Join(round6AdjudicationDir, "true-gaps.json"))
	if trueGaps.SchemaVersion != 1 || trueGaps.SourceRound != "round6" || trueGaps.Split != "dev" ||
		trueGaps.Cases == nil || len(trueGaps.Cases) != counts["true_gap"] || strings.TrimSpace(trueGaps.Reason) == "" {
		t.Fatalf("invalid true-gap artifact: %#v", trueGaps)
	}
}

func TestRound13DevFollowupRecordsContentIdenticalAliasOnly(t *testing.T) {
	corpus, err := searcheval.LoadCorpus("corpus/v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var base searcheval.Case
	for _, testCase := range corpus.Cases {
		if testCase.ID == "python-exceptions-tutorial" {
			base = testCase
			break
		}
	}
	if base.ID == "" || base.Split != "dev" {
		t.Fatal("follow-up case is not in the frozen dev split")
	}

	judgment := readStrictJSONFile[followupAliasJudgment](t, filepath.Join(round13AdjudicationDir, "judgment.json"))
	if judgment.SchemaVersion != 1 || judgment.CaseID != base.ID || judgment.Query != base.Query ||
		judgment.Classification != "alias" || judgment.SourceRound.ID != "round13-freeze-dev" ||
		judgment.SourceRound.SHA256 != "5a82448175e598febf874f087c352857abeec3c279e978e4d230dc850c870834" ||
		judgment.Observed.Rank != 1 || judgment.Target.ContentHash == "" ||
		judgment.Target.ContentHash != judgment.Observed.ContentHash ||
		judgment.Target.CompressedBodyBytes != judgment.Observed.CompressedBodyBytes ||
		judgment.Target.Title != judgment.Observed.Title || len(judgment.TargetAdditions) != 1 ||
		strings.TrimSpace(judgment.Rationale) == "" || !containsTargetURL(base.Targets, judgment.Target.URL) {
		t.Fatalf("invalid round13 alias judgment: %#v", judgment)
	}

	overlays := readStrictJSONLines[adjudicationOverlay](t, filepath.Join(round13AdjudicationDir, "dev-targets-v2-overlay.jsonl"))
	if len(overlays) != 1 || overlays[0].ID != base.ID {
		t.Fatalf("round13 overlays = %#v", overlays)
	}
	wantTargets := append(append([]searcheval.Target(nil), base.Targets...), judgment.TargetAdditions...)
	if !reflect.DeepEqual(overlays[0].Targets, wantTargets) {
		t.Fatalf("round13 overlay targets = %#v, want %#v", overlays[0].Targets, wantTargets)
	}
}

func containsTargetURL(targets []searcheval.Target, wanted string) bool {
	for _, target := range targets {
		if target.URL == wanted {
			return true
		}
	}
	return false
}

func readStrictJSONFile[T any](t *testing.T, path string) T {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return decodeStrictTestJSON[T](t, contents)
}

func readStrictJSONLines[T any](t *testing.T, path string) []T {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	lines := make([]T, 0)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			t.Fatalf("%s contains an empty JSONL line", path)
		}
		lines = append(lines, decodeStrictTestJSON[T](t, scanner.Bytes()))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func decodeStrictTestJSON[T any](t *testing.T, contents []byte) T {
	t.Helper()
	var value T
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("JSON has trailing data: %v", err)
	}
	return value
}
