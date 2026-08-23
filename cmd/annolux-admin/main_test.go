package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/eason4kim-rocket/annolux/searchindex"
)

func TestOptionalLimit(t *testing.T) {
	if value, err := optionalLimit([]string{"ledger", "account"}, 2, 100); err != nil || value != 100 {
		t.Fatalf("default = %d, %v", value, err)
	}
	if value, err := optionalLimit([]string{"ledger", "account", "25"}, 2, 100); err != nil || value != 25 {
		t.Fatalf("explicit = %d, %v", value, err)
	}
	if _, err := optionalLimit([]string{"ledger", "account", "0"}, 2, 100); err == nil {
		t.Fatal("accepted zero")
	}
}

func TestAuditIndexChecksIntegrityAndFTSCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	store, err := searchindex.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []searchindex.Page{
		{URL: "https://example.com/en", Root: "example.com", Title: "English", Body: "search index", Lang: searchindex.LangEnglish, FetchedAt: time.Now()},
		{URL: "https://example.com/zh", Root: "example.com", Title: "中文", Body: "搜索索引", Lang: searchindex.LangChinese, FetchedAt: time.Now()},
	} {
		if err := store.Upsert(context.Background(), page); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	audit, err := auditIndex(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if audit.Integrity != "ok" || audit.Pages != 2 || audit.EnglishPages != 1 || audit.ChinesePages != 1 || audit.EnglishFTS != 1 || audit.ChineseFTS != 1 || len(audit.SHA256) != 64 {
		t.Fatalf("audit = %#v", audit)
	}
}
