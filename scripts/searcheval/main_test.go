package main

import (
	"testing"

	"github.com/eason4kim-rocket/annolux/internal/searcheval"
)

func TestFrozenCorpusV1LoadsWithDeclaredStrata(t *testing.T) {
	corpus, err := searcheval.LoadCorpus("corpus/v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 100 {
		t.Fatalf("cases = %d", len(corpus.Cases))
	}
	dev, err := corpus.CasesForSplit("dev")
	if err != nil {
		t.Fatal(err)
	}
	heldout, err := corpus.CasesForSplit("heldout")
	if err != nil {
		t.Fatal(err)
	}
	if len(dev) != 60 || len(heldout) != 40 {
		t.Fatalf("split sizes = %d/%d", len(dev), len(heldout))
	}
	if corpus.Digest != corpus.Manifest.CasesSHA256 {
		t.Fatalf("digest = %s", corpus.Digest)
	}
	if corpus.Manifest.Protocol.RequestIntervalMS != 250 {
		t.Fatalf("request interval = %d", corpus.Manifest.Protocol.RequestIntervalMS)
	}
	if len(corpus.ManifestDigest) != 64 {
		t.Fatalf("manifest digest = %q", corpus.ManifestDigest)
	}
}
