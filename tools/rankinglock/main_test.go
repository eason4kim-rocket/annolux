package main

import "testing"

func TestRepositoryRankingLock(t *testing.T) {
	if err := verify("../../searchindex/ranking.lock.json"); err != nil {
		t.Fatal(err)
	}
}
