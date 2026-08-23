package searcheval

import "testing"

func TestCanonicalURLKeyIgnoresTransportAndCosmeticAliases(t *testing.T) {
	left, err := CanonicalURLKey("http://www.Example.com:80/docs/page/?b=2&a=1")
	if err != nil {
		t.Fatal(err)
	}
	right, err := CanonicalURLKey("https://example.com/docs/page?a=1&b=2")
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("keys differ: %q != %q", left, right)
	}
}

func TestCanonicalURLKeyPreservesMeaningfulPath(t *testing.T) {
	stable, err := CanonicalURLKey("https://doc.rust-lang.org/book/ch09.html")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := CanonicalURLKey("https://doc.rust-lang.org/beta/book/ch09.html")
	if err != nil {
		t.Fatal(err)
	}
	if stable == beta {
		t.Fatal("stable and beta documentation collapsed")
	}
}

func TestCanonicalURLKeyRejectsCredentialsAndFragments(t *testing.T) {
	for _, rawURL := range []string{
		"https://user:secret@example.com/docs",
		"https://example.com/docs#section",
		"file:///tmp/index.db",
	} {
		if _, err := CanonicalURLKey(rawURL); err == nil {
			t.Fatalf("CanonicalURLKey(%q) succeeded", rawURL)
		}
	}
}
