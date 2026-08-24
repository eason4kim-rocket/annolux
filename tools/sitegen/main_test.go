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

func TestLinkifyPlainWrapsAnnoluxEmails(t *testing.T) {
	got := string(linkifyPlain(`Email support@annolux.com. Do not send <keys>.`))
	if !strings.Contains(got, `<a href="mailto:support@annolux.com">support@annolux.com</a>`) {
		t.Fatalf("mailto missing: %s", got)
	}
	if !strings.Contains(got, `&lt;keys&gt;`) || strings.Contains(got, `<keys>`) {
		t.Fatalf("plain text was not escaped: %s", got)
	}
}

func TestDocsNavMarksCurrentAndLocalizes(t *testing.T) {
	if !isDocsPage("mcp/cursor") || !isDocsPage("integrations/dify") || isDocsPage("pricing") || isDocsPage("blog") {
		t.Fatal("isDocsPage mismatch")
	}
	if !isMCPNavHighlight("mcp/zed") || isMCPNavHighlight("docs") {
		t.Fatal("MCP top-nav highlight mismatch")
	}
	if !isDocsNavHighlight("integrations/langchain") || isDocsNavHighlight("mcp") {
		t.Fatal("Docs top-nav highlight mismatch")
	}

	nav := docsNav("en", "mcp/cursor")
	if len(nav) != 3 {
		t.Fatalf("groups = %d, want 3", len(nav))
	}
	if nav[0].Label != "Getting started" || nav[1].Label != "MCP clients" || nav[2].Label != "Integrations" {
		t.Fatalf("en labels = %#v", []string{nav[0].Label, nav[1].Label, nav[2].Label})
	}
	current := 0
	for _, group := range nav {
		for _, item := range group.Items {
			if item.Current {
				current++
				if item.Href != "/mcp/cursor" || item.Label != "Cursor" {
					t.Fatalf("current item = %+v", item)
				}
			}
		}
	}
	if current != 1 {
		t.Fatalf("current count = %d, want 1", current)
	}

	zh := docsNav("zh", "docs")
	if zh[0].Label != "开始" || zh[0].Items[0].Href != "/zh/docs" || !zh[0].Items[0].Current {
		t.Fatalf("zh nav = %+v", zh[0])
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
