package searcheval

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteExclusiveDoesNotReplaceBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round.jsonl")
	if err := WriteExclusive(path, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := WriteExclusive(path, []byte("second\n")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second WriteExclusive error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "first\n" {
		t.Fatalf("baseline was replaced: %q", contents)
	}
}

func TestEncodeJSONLIncludesRunCasesAndSummary(t *testing.T) {
	report := RunReport{
		Metadata: RunMetadata{Type: "run", SchemaVersion: 1, RunID: "round-one"},
		Cases:    []CaseResult{{Type: "case", ID: "case-one", Results: []ObservedResult{}}},
		Summary:  Summary{Type: "summary"},
	}
	encoded, err := EncodeJSONL(report)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := countNewlines(encoded), 3; got != want {
		t.Fatalf("JSONL lines = %d, want %d", got, want)
	}
}

func countNewlines(contents []byte) int {
	count := 0
	for _, value := range contents {
		if value == '\n' {
			count++
		}
	}
	return count
}
