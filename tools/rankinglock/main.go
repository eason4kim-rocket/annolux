// Command rankinglock verifies that the frozen local ranking implementation
// still matches its reviewed source digests.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type lockFile struct {
	SchemaVersion int               `json:"schema_version"`
	FrozenAt      string            `json:"frozen_at"`
	Files         map[string]string `json:"files"`
	Policy        string            `json:"policy"`
}

func main() {
	if err := verify("searchindex/ranking.lock.json"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "rankinglock: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("ranking lock: pass")
}

func verify(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	var lock lockFile
	if err := decoder.Decode(&lock); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("lock contains trailing JSON")
	}
	if lock.SchemaVersion != 1 || lock.FrozenAt == "" || strings.TrimSpace(lock.Policy) == "" || len(lock.Files) == 0 {
		return errors.New("lock metadata is invalid")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(path), ".."))
	paths := make([]string, 0, len(lock.Files))
	for name := range lock.Files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	for _, name := range paths {
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean != name || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.Ext(clean) != ".go" {
			return fmt.Errorf("unsafe locked path %q", name)
		}
		data, err := os.ReadFile(filepath.Join(repositoryRoot, clean))
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != lock.Files[name] {
			return fmt.Errorf("%s does not match the frozen digest", name)
		}
	}
	return nil
}
