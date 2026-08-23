package indexer

import (
	"encoding/json"
	"os"
	"testing"
)

// TestHeadCoverageSeedsPassAdmission pins every head-coverage seed against
// the crawler's own junk rule: a seed the admission rejects would silently
// contribute nothing, and that must fail loudly here instead.
func TestHeadCoverageSeedsPassAdmission(t *testing.T) {
	raw, err := os.ReadFile("../ops/seeds-head-coverage.json")
	if err != nil {
		t.Skipf("seed file not present: %v", err)
	}
	var seeds []string
	if err := json.Unmarshal(raw, &seeds); err != nil {
		t.Fatalf("seed file invalid: %v", err)
	}
	if len(seeds) < 50 {
		t.Fatalf("suspiciously few seeds: %d", len(seeds))
	}
	for _, seed := range seeds {
		if JunkURL(seed) {
			t.Errorf("seed rejected by admission: %s (%s)", seed, JunkReason(seed))
		}
	}
}
