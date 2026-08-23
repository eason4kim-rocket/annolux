// Package searcheval provides a frozen, target-retrieval evaluation protocol
// for Annolux's local Search index. It deliberately does not make exhaustive
// relevance claims about unjudged results.
package searcheval

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	SchemaVersion      = 1
	maximumManifestLen = 64 << 10
	maximumCasesLen    = 2 << 20
	maximumCaseLineLen = 64 << 10
)

var (
	identifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,79}$`)
	stratumPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{1,79}$`)
)

// Manifest freezes both the corpus bytes and the evaluation contract.
type Manifest struct {
	SchemaVersion int            `json:"schema_version"`
	Name          string         `json:"name"`
	FrozenAt      string         `json:"frozen_at"`
	CasesFile     string         `json:"cases_file"`
	CasesSHA256   string         `json:"cases_sha256"`
	Expected      ExpectedCounts `json:"expected"`
	Protocol      Protocol       `json:"protocol"`
}

// ExpectedCounts prevents a corpus edit from silently changing its strata.
type ExpectedCounts struct {
	Total      int            `json:"total"`
	Splits     map[string]int `json:"splits"`
	Languages  map[string]int `json:"languages"`
	Categories map[string]int `json:"categories"`
	Tags       map[string]int `json:"tags,omitempty"`
}

// Protocol is the frozen target-ranking interpretation used for every round.
type Protocol struct {
	Limit             int    `json:"limit"`
	RequestIntervalMS int    `json:"request_interval_ms"`
	StrongGrade       int    `json:"strong_grade"`
	RelevantGrade     int    `json:"relevant_grade"`
	HeldoutPolicy     string `json:"heldout_policy"`
}

// Case is one immutable query and its finite set of judged target pages.
type Case struct {
	ID       string   `json:"id"`
	Split    string   `json:"split"`
	Language string   `json:"language"`
	Category string   `json:"category"`
	Query    string   `json:"query"`
	Targets  []Target `json:"targets"`
	Tags     []string `json:"tags,omitempty"`
}

// Target is a known relevant page. Grade 3 is primary, grade 2 is acceptable,
// and grade 1 is a weak alternative. Unlisted results remain unjudged.
type Target struct {
	URL       string `json:"url"`
	Relevance int    `json:"relevance"`
}

// Corpus is the validated, digest-pinned input to one evaluation round.
type Corpus struct {
	Manifest       Manifest
	Cases          []Case
	Digest         string
	ManifestDigest string
	Path           string
}

// LoadCorpus reads a strict manifest and its relative JSONL case file.
func LoadCorpus(manifestPath string) (Corpus, error) {
	manifestBytes, err := readBoundedFile(manifestPath, maximumManifestLen)
	if err != nil {
		return Corpus{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest Manifest
	if err := decodeStrictJSON(manifestBytes, &manifest); err != nil {
		return Corpus{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return Corpus{}, err
	}

	manifestDir, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		return Corpus{}, fmt.Errorf("resolve manifest directory: %w", err)
	}
	casesPath := filepath.Join(manifestDir, filepath.FromSlash(manifest.CasesFile))
	resolvedCasesPath, err := filepath.Abs(casesPath)
	if err != nil {
		return Corpus{}, fmt.Errorf("resolve cases path: %w", err)
	}
	if resolvedCasesPath != manifestDir && !strings.HasPrefix(resolvedCasesPath, manifestDir+string(os.PathSeparator)) {
		return Corpus{}, errors.New("cases file escapes manifest directory")
	}
	casesBytes, err := readBoundedFile(resolvedCasesPath, maximumCasesLen)
	if err != nil {
		return Corpus{}, fmt.Errorf("read cases: %w", err)
	}
	digestBytes := sha256.Sum256(casesBytes)
	digest := hex.EncodeToString(digestBytes[:])
	if !strings.EqualFold(digest, manifest.CasesSHA256) {
		return Corpus{}, fmt.Errorf("cases digest mismatch: got %s", digest)
	}
	cases, err := decodeCases(casesBytes)
	if err != nil {
		return Corpus{}, err
	}
	if err := validateCases(manifest, cases); err != nil {
		return Corpus{}, err
	}
	manifestDigestBytes := sha256.Sum256(manifestBytes)
	return Corpus{
		Manifest: manifest, Cases: cases, Digest: digest,
		ManifestDigest: hex.EncodeToString(manifestDigestBytes[:]), Path: resolvedCasesPath,
	}, nil
}

// CasesForSplit returns a copy of the requested frozen split. "all" preserves
// corpus order and is the only mode intended for a release-gate baseline.
func (corpus Corpus) CasesForSplit(split string) ([]Case, error) {
	switch split {
	case "all":
		return append([]Case(nil), corpus.Cases...), nil
	case "dev", "heldout":
		filtered := make([]Case, 0, corpus.Manifest.Expected.Splits[split])
		for _, testCase := range corpus.Cases {
			if testCase.Split == split {
				filtered = append(filtered, testCase)
			}
		}
		return filtered, nil
	default:
		return nil, errors.New("split must be all, dev, or heldout")
	}
}

func validateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported manifest schema version %d", manifest.SchemaVersion)
	}
	if !identifierPattern.MatchString(manifest.Name) {
		return errors.New("manifest name is invalid")
	}
	if _, err := time.Parse(time.DateOnly, manifest.FrozenAt); err != nil {
		return errors.New("manifest frozen_at is invalid")
	}
	if manifest.CasesFile == "" || filepath.IsAbs(manifest.CasesFile) || filepath.Clean(manifest.CasesFile) != manifest.CasesFile {
		return errors.New("manifest cases_file is invalid")
	}
	if len(manifest.CasesSHA256) != sha256.Size*2 {
		return errors.New("manifest cases_sha256 is invalid")
	}
	if _, err := hex.DecodeString(manifest.CasesSHA256); err != nil {
		return errors.New("manifest cases_sha256 is invalid")
	}
	if manifest.Expected.Total < 1 || manifest.Expected.Total > 10_000 {
		return errors.New("manifest expected total is invalid")
	}
	if manifest.Protocol.Limit < 1 || manifest.Protocol.Limit > 20 ||
		manifest.Protocol.RequestIntervalMS < 0 || manifest.Protocol.RequestIntervalMS > 60_000 ||
		manifest.Protocol.StrongGrade < 1 || manifest.Protocol.StrongGrade > 3 ||
		manifest.Protocol.RelevantGrade < 1 || manifest.Protocol.RelevantGrade > manifest.Protocol.StrongGrade ||
		strings.TrimSpace(manifest.Protocol.HeldoutPolicy) == "" {
		return errors.New("manifest protocol is invalid")
	}
	for name, counts := range map[string]map[string]int{
		"splits": manifest.Expected.Splits, "languages": manifest.Expected.Languages,
		"categories": manifest.Expected.Categories, "tags": manifest.Expected.Tags,
	} {
		for key, count := range counts {
			if !stratumPattern.MatchString(key) || count < 1 || count > manifest.Expected.Total {
				return fmt.Errorf("manifest expected %s is invalid", name)
			}
		}
	}
	return nil
}

func decodeCases(contents []byte) ([]Case, error) {
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 4096), maximumCaseLineLen)
	cases := make([]Case, 0, 128)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			return nil, fmt.Errorf("case line %d is empty", lineNumber)
		}
		var testCase Case
		if err := decodeStrictJSON(line, &testCase); err != nil {
			return nil, fmt.Errorf("decode case line %d: %w", lineNumber, err)
		}
		cases = append(cases, testCase)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan cases: %w", err)
	}
	return cases, nil
}

func validateCases(manifest Manifest, cases []Case) error {
	if len(cases) != manifest.Expected.Total {
		return fmt.Errorf("case count %d does not match manifest total %d", len(cases), manifest.Expected.Total)
	}
	ids := make(map[string]struct{}, len(cases))
	queries := make(map[string]struct{}, len(cases))
	splits := make(map[string]int)
	languages := make(map[string]int)
	categories := make(map[string]int)
	tags := make(map[string]int)
	for index, testCase := range cases {
		if err := validateCase(testCase); err != nil {
			return fmt.Errorf("case %d (%s): %w", index+1, testCase.ID, err)
		}
		if _, exists := ids[testCase.ID]; exists {
			return fmt.Errorf("duplicate case id %q", testCase.ID)
		}
		ids[testCase.ID] = struct{}{}
		queryKey := strings.ToLower(testCase.Query)
		if _, exists := queries[queryKey]; exists {
			return fmt.Errorf("duplicate query %q", testCase.Query)
		}
		queries[queryKey] = struct{}{}
		splits[testCase.Split]++
		languages[testCase.Language]++
		categories[testCase.Category]++
		for _, tag := range testCase.Tags {
			tags[tag]++
		}
	}
	countsByDimension := map[string]struct {
		got  map[string]int
		want map[string]int
	}{
		"splits":     {splits, manifest.Expected.Splits},
		"languages":  {languages, manifest.Expected.Languages},
		"categories": {categories, manifest.Expected.Categories},
	}
	for name, counts := range countsByDimension {
		if !equalCounts(counts.got, counts.want) {
			return fmt.Errorf("case %s counts do not match manifest: got %v want %v", name, counts.got, counts.want)
		}
	}
	if !equalCounts(tags, manifest.Expected.Tags) {
		return fmt.Errorf("case tag counts do not match manifest: got %v want %v", tags, manifest.Expected.Tags)
	}
	return nil
}

func validateCase(testCase Case) error {
	if !identifierPattern.MatchString(testCase.ID) {
		return errors.New("id is invalid")
	}
	if testCase.Split != "dev" && testCase.Split != "heldout" {
		return errors.New("split is invalid")
	}
	if testCase.Language != "en" && testCase.Language != "zh" && testCase.Language != "mixed" {
		return errors.New("language is invalid")
	}
	if testCase.Category != "technical" && testCase.Category != "reference" &&
		testCase.Category != "news" && testCase.Category != "release" {
		return errors.New("category is invalid")
	}
	if testCase.Query == "" || strings.Join(strings.Fields(testCase.Query), " ") != testCase.Query || len([]rune(testCase.Query)) > 200 {
		return errors.New("query is invalid")
	}
	if len(testCase.Targets) == 0 || len(testCase.Targets) > 8 {
		return errors.New("targets are invalid")
	}
	targets := make(map[string]struct{}, len(testCase.Targets))
	hasPrimary := false
	for _, target := range testCase.Targets {
		if target.Relevance < 1 || target.Relevance > 3 {
			return errors.New("target relevance is invalid")
		}
		key, err := CanonicalURLKey(target.URL)
		if err != nil {
			return fmt.Errorf("target URL is invalid: %w", err)
		}
		if _, exists := targets[key]; exists {
			return errors.New("target URLs contain a duplicate identity")
		}
		targets[key] = struct{}{}
		hasPrimary = hasPrimary || target.Relevance == 3
	}
	if !hasPrimary {
		return errors.New("case has no primary target")
	}
	if !sort.StringsAreSorted(testCase.Tags) {
		return errors.New("tags are not sorted")
	}
	for index, tag := range testCase.Tags {
		if !identifierPattern.MatchString(tag) || index > 0 && tag == testCase.Tags[index-1] {
			return errors.New("tags are invalid")
		}
	}
	return nil
}

// CanonicalURLKey intentionally ignores HTTP versus HTTPS, a leading www,
// fragments, default ports, and a non-root trailing slash. It does not merge
// distinct content paths or locale/version segments.
func CanonicalURLKey(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Hostname() == "" {
		return "", errors.New("URL must be an absolute HTTP URL without credentials")
	}
	if parsed.Fragment != "" {
		return "", errors.New("URL fragment is not allowed")
	}
	hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	hostname = strings.TrimPrefix(hostname, "www.")
	port := parsed.Port()
	if port == "80" && parsed.Scheme == "http" || port == "443" && parsed.Scheme == "https" {
		port = ""
	}
	host := hostname
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	query := parsed.Query().Encode()
	if query != "" {
		path += "?" + query
	}
	return host + path, nil
}

func readBoundedFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > maximum {
		return nil, errors.New("file exceeds size limit")
	}
	return contents, nil
}

func decodeStrictJSON(contents []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func equalCounts(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
