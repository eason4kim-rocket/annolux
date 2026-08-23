package indexer

import "testing"

func TestJunkURLMatchesInternalRule(t *testing.T) {
	for _, candidate := range []string{
		"https://developer.mozilla.org/en-US/docs/Web/API",
		"https://developer.mozilla.org/de/docs/Glossary/Flex",
		"https://ab.wikipedia.org/wiki/Article",
		"https://smalltalk.gnu.org/manual-base/manual-libs/manual-base/manual-libs/page",
		"https://example.org/paper.pdf",
		"::not a url::",
	} {
		if JunkURL(candidate) != junkURL(candidate) {
			t.Fatalf("JunkURL disagrees with junkURL for %q", candidate)
		}
	}
}

func TestJunkReasonAttribution(t *testing.T) {
	cases := map[string]string{
		"https://developer.mozilla.org/en-US/docs/Web/API":                               "",
		"https://example.org/paper.pdf":                                                  JunkReasonAssetExtension,
		"https://developer.mozilla.org/de/docs/Glossary/Flex":                            JunkReasonForeignLocalePath,
		"https://ab.wikipedia.org/wiki/Article":                                          JunkReasonForeignLocaleHost,
		"https://smalltalk.gnu.org/manual-base/manual-libs/manual-base/manual-libs/page": JunkReasonTrappedPath,
		"https://vger.kernel.org/_sources/index":                                         JunkReasonTrappedPath,
	}
	for candidate, want := range cases {
		if got := JunkReason(candidate); got != want {
			t.Fatalf("JunkReason(%q) = %q, want %q", candidate, got, want)
		}
	}
}

// A URL the rule accepts must never be given a reason, so the reason labels
// can never be mistaken for a second admission test.
func TestJunkReasonIsEmptyForAcceptedURLs(t *testing.T) {
	for _, candidate := range []string{
		"https://kubernetes.io/docs/concepts/",
		"https://go.dev/ref/spec",
		"https://zh.wikipedia.org/wiki/Article",
	} {
		if JunkURL(candidate) {
			t.Fatalf("%q is unexpectedly junk", candidate)
		}
		if reason := JunkReason(candidate); reason != "" {
			t.Fatalf("JunkReason(%q) = %q, want empty", candidate, reason)
		}
	}
}
