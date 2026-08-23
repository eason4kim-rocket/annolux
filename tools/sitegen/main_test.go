package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildManifestRejectsStaleSourcesAndOutput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	out := filepath.Join(root, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, source, "source-v1")
	output := filepath.Join(out, "index.html")
	writeTestFile(t, output, "output-v1")

	if err := writeBuildManifest(out, []string{source}, false); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := checkBuildManifest(out, []string{source}, false); err != nil {
		t.Fatalf("fresh site rejected: %v", err)
	}

	writeTestFile(t, source, "source-v2")
	if err := checkBuildManifest(out, []string{source}, false); err == nil || !strings.Contains(err.Error(), "source files changed") {
		t.Fatalf("stale source error = %v", err)
	}

	writeTestFile(t, source, "source-v1")
	writeTestFile(t, output, "output-v2")
	if err := checkBuildManifest(out, []string{source}, false); err == nil || !strings.Contains(err.Error(), "generated files changed") {
		t.Fatalf("stale output error = %v", err)
	}
}

func TestBuildManifestReleaseGate(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	out := filepath.Join(root, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, source, "source")
	writeTestFile(t, filepath.Join(out, "index.html"), "output")

	if err := writeBuildManifest(out, []string{source}, false); err != nil {
		t.Fatal(err)
	}
	if err := checkBuildManifest(out, []string{source}, true); err == nil || !strings.Contains(err.Error(), "not a release build") {
		t.Fatalf("development build passed release gate: %v", err)
	}

	if err := writeBuildManifest(out, []string{source}, true); err != nil {
		t.Fatal(err)
	}
	if err := checkBuildManifest(out, []string{source}, true); err != nil {
		t.Fatalf("release build rejected: %v", err)
	}
}

func TestBuildManifestRejectsMissingAndTrailingData(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	out := filepath.Join(root, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, source, "source")
	writeTestFile(t, filepath.Join(out, "index.html"), "output")

	if err := checkBuildManifest(out, []string{source}, false); err == nil || !strings.Contains(err.Error(), "manifest missing") {
		t.Fatalf("missing manifest error = %v", err)
	}
	if err := writeBuildManifest(out, []string{source}, false); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(out, siteBuildManifestName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(data, []byte("{}")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkBuildManifest(out, []string{source}, false); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("trailing manifest data error = %v", err)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
