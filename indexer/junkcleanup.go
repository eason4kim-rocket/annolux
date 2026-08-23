package indexer

import (
	"net/url"
	"path"
	"strings"
)

// Junk reason labels. They describe why an already-rejected URL was rejected;
// they are reporting detail, never an admission decision of their own.
const (
	JunkReasonUnparsable        = "unparsable"
	JunkReasonAssetExtension    = "asset_extension"
	JunkReasonForeignLocalePath = "foreign_locale_path"
	JunkReasonForeignLocaleHost = "foreign_locale_host"
	JunkReasonTrappedPath       = "trapped_path"
)

// JunkURL exposes the crawler's junk decision so a retroactive index cleanup
// judges stored rows by exactly the rule that admits new links. Historical
// rows predate later rule tightenings, so the cleanup has to reuse this
// function rather than carry a second copy of it that can drift.
func JunkURL(rawURL string) bool {
	return junkURL(rawURL)
}

// JunkReason attributes a URL that JunkURL already rejected to the rule that
// rejected it, following the same order junkURL evaluates them in. It reports
// an empty label for a URL that is not junk, so it can never be mistaken for
// a second admission test.
func JunkReason(rawURL string) string {
	if !junkURL(rawURL) {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return JunkReasonUnparsable
	}
	if _, skip := assetExtensions[strings.ToLower(path.Ext(parsed.Path))]; skip {
		return JunkReasonAssetExtension
	}
	switch {
	case foreignLocalePath(rawURL):
		return JunkReasonForeignLocalePath
	case foreignLocaleHost(parsed.Hostname()):
		return JunkReasonForeignLocaleHost
	default:
		return JunkReasonTrappedPath
	}
}
