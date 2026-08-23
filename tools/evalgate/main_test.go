package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFrozenGateAndRankingLock(t *testing.T) {
	gate, err := readGate("../../scripts/searcheval/corpus/v2/release-gate.json")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := fileDigest("../../searchindex/ranking.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	if digest != gate.RankingLockSHA256 {
		t.Fatalf("ranking lock digest = %s, gate = %s", digest, gate.RankingLockSHA256)
	}
}

func TestWriteExclusiveDoesNotReplaceResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	if err := writeExclusive(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := writeExclusive(path, []byte("second")); err == nil {
		t.Fatal("replaced immutable gate result")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "first" {
		t.Fatalf("result = %q, %v", data, err)
	}
}
