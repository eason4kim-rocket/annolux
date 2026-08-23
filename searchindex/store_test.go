package searchindex

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestOpenCreatesSchemaAndUpsertsWithoutDuplicateHash(t *testing.T) {
	store := openTestStore(t)
	page := Page{
		URL: "https://example.com/a", Root: "example.com", Title: "Alpha",
		Body: "hello world", Lang: LangEnglish, FetchedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	if err := store.Upsert(context.Background(), page); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if err := store.Upsert(context.Background(), page); err != nil {
		t.Fatalf("duplicate Upsert() error = %v", err)
	}
	count, err := store.CountPages(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("CountPages() = %d, %v", count, err)
	}

	page.Body = "hello world changed"
	page.ContentHash = ""
	if err := store.Upsert(context.Background(), page); err != nil {
		t.Fatalf("changed Upsert() error = %v", err)
	}
	if count, err = store.CountPages(context.Background()); err != nil || count != 1 {
		t.Fatalf("CountPages after change = %d, %v", count, err)
	}

	page.URL = "https://example.com/b"
	page.Lang = LangChinese
	page.Body = "你好世界"
	page.ContentHash = ""
	if err := store.Upsert(context.Background(), page); err != nil {
		t.Fatalf("second page Upsert() error = %v", err)
	}
	if count, err = store.CountPages(context.Background()); err != nil || count != 2 {
		t.Fatalf("CountPages two pages = %d, %v", count, err)
	}
}

func TestOpenRejectsEmptyPathAndClosedStore(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("Open(\"\") succeeded")
	}
	store := openTestStore(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(context.Background(), Page{URL: "https://example.com/", Root: "example.com", Lang: LangEnglish}); err != ErrClosed {
		t.Fatalf("Upsert on closed store = %v", err)
	}
}

func TestOpenReadOnlyQueriesWithoutChangingIndex(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "index.db")
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	page := Page{
		URL: "https://example.com/readonly", Root: "example.com", Title: "Read only",
		Body: "hello immutable index", Lang: LangEnglish,
	}
	if err := writer.Upsert(context.Background(), page); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}

	beforeHash := fileSHA256(t, path)
	beforeMode := fileMode(t, path)
	beforeEntries := directoryEntries(t, directory)
	reader, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly() error = %v", err)
	}
	count, err := reader.CountPages(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("CountPages() = %d, %v", count, err)
	}
	hits, err := reader.Query(context.Background(), "hello immutable", 10)
	if err != nil || len(hits) != 1 || hits[0].URL != page.URL {
		t.Fatalf("Query() = %#v, %v", hits, err)
	}
	if err := reader.Upsert(context.Background(), Page{
		URL: "https://example.com/write", Root: "example.com", Lang: LangEnglish,
	}); err == nil {
		t.Fatal("Upsert() through read-only handle succeeded")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	if got := fileSHA256(t, path); got != beforeHash {
		t.Fatalf("database hash changed: before=%s after=%s", beforeHash, got)
	}
	if got := fileMode(t, path); got != beforeMode {
		t.Fatalf("database mode changed: before=%v after=%v", beforeMode, got)
	}
	if got := directoryEntries(t, directory); !reflect.DeepEqual(got, beforeEntries) {
		t.Fatalf("database sidecars changed: before=%v after=%v", beforeEntries, got)
	}
}

func TestOpenReadOnlyRejectsUnsafeFiles(t *testing.T) {
	t.Run("non-empty WAL", func(t *testing.T) {
		path := createClosedIndex(t)
		if err := os.WriteFile(path+"-wal", []byte("uncheckpointed"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenReadOnly(path); !errors.Is(err, ErrUnsafeReadOnly) {
			t.Fatalf("OpenReadOnly() error = %v, want ErrUnsafeReadOnly", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		path := createClosedIndex(t)
		link := filepath.Join(filepath.Dir(path), "index-link.db")
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenReadOnly(link); !errors.Is(err, ErrUnsafeReadOnly) {
			t.Fatalf("OpenReadOnly() error = %v, want ErrUnsafeReadOnly", err)
		}
	})
}

func TestOpenReadOnlyRejectsUnknownSchema(t *testing.T) {
	path := createClosedIndex(t)
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(99, 'test')`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenReadOnly(path); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("OpenReadOnly() error = %v, want ErrSchemaMismatch", err)
	}
}

func TestUpsertManyWritesChangedPagesOnly(t *testing.T) {
	store := openTestStore(t)
	pages := []Page{
		{URL: "https://example.com/a", Root: "example.com", Title: "A", Body: "alpha", Lang: LangEnglish},
		{URL: "https://example.com/b", Root: "example.com", Title: "B", Body: "bravo", Lang: LangEnglish},
	}
	written, err := store.UpsertMany(context.Background(), pages)
	if err != nil || written != 2 {
		t.Fatalf("UpsertMany() = %d, %v", written, err)
	}
	written, err = store.UpsertMany(context.Background(), pages)
	if err != nil || written != 0 {
		t.Fatalf("duplicate UpsertMany() = %d, %v", written, err)
	}
	pages[1].Body = "bravo changed"
	pages[1].ContentHash = ""
	written, err = store.UpsertMany(context.Background(), pages)
	if err != nil || written != 1 {
		t.Fatalf("changed UpsertMany() = %d, %v", written, err)
	}
}

func TestNormalizePageRequiresURLRootAndLang(t *testing.T) {
	if _, err := normalizePage(Page{}); err == nil {
		t.Fatal("empty page accepted")
	}
	if _, err := normalizePage(Page{URL: "https://example.com/", Root: "example.com", Lang: "de"}); err == nil {
		t.Fatal("unknown lang accepted")
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createClosedIndex(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}

func directoryEntries(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}
