package searcheval

import "testing"

// TestV3CorpusLoads pins the frozen v3 corpus to the loader's full validation:
// counts, hashes, identifiers, tag ordering, and target URLs must all hold.
func TestV3CorpusLoads(t *testing.T) {
	corpus, err := LoadCorpus("../../scripts/searcheval/corpus/v3/manifest.json")
	if err != nil {
		t.Fatalf("v3 corpus is invalid: %v", err)
	}
	if len(corpus.Cases) != 100 {
		t.Fatalf("v3 cases = %d, want 100", len(corpus.Cases))
	}
}
