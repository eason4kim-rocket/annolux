// Command repopolicy rejects public infrastructure IP literals from the
// source tree. Loopback, private/reserved test networks, and a tiny explicit
// set of well-known resolver/example addresses used by network-policy tests
// are allowed; an operator or VPS address is not.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var ipv4Pattern = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
var inlineSVGPathPattern = regexp.MustCompile(`<path\b[^>]*>`)

var allowedPublicAddresses = map[netip.Addr]struct{}{
	netip.MustParseAddr("1.1.1.1"):       {},
	netip.MustParseAddr("8.8.8.8"):       {},
	netip.MustParseAddr("93.184.216.34"): {},
}

var allowedSpecialNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

func main() {
	root := "."
	if len(os.Args) == 2 {
		root = os.Args[1]
	} else if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: repopolicy [ROOT]")
		os.Exit(2)
	}
	violations, err := scanPublicIPv4(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(violations) != 0 {
		for _, violation := range violations {
			fmt.Fprintln(os.Stderr, violation)
		}
		fmt.Fprintln(os.Stderr, "unapproved public infrastructure IP literal found")
		os.Exit(1)
	}
}

func scanPublicIPv4(root string) ([]string, error) {
	var violations []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "bin" || entry.Name() == "node_modules" || entry.Name() == "dist" && filepath.Base(filepath.Dir(path)) != "website" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "OPS.local.md" {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(content, 0) >= 0 {
			return nil
		}
		for _, line := range strings.Split(string(content), "\n") {
			// Decimal coordinate streams in inline SVG paths can accidentally
			// resemble dotted IPv4. Remove only the path element before scanning
			// the rest of the source line.
			line = inlineSVGPathPattern.ReplaceAllString(line, "")
			for _, raw := range ipv4Pattern.FindAllString(line, -1) {
				address, err := netip.ParseAddr(raw)
				if err != nil || !address.Is4() || allowedAddress(address) {
					continue
				}
				violations = append(violations, path+": "+raw)
			}
		}
		return nil
	})
	if err != nil {
		return nil, errors.New("repository policy scan failed: " + err.Error())
	}
	return violations, nil
}

func allowedAddress(address netip.Addr) bool {
	if address.IsLoopback() || address.IsPrivate() || address.IsUnspecified() {
		return true
	}
	if _, allowed := allowedPublicAddresses[address]; allowed {
		return true
	}
	for _, network := range allowedSpecialNetworks {
		if network.Contains(address) {
			return true
		}
	}
	return false
}
