// Command evalgate checks one immutable Search evaluation artifact against the
// frozen release thresholds and ranking-source lock. It never changes ranking
// weights or the corpus.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/eason4kim-rocket/annolux/internal/searcheval"
)

type releaseGate struct {
	SchemaVersion     int    `json:"schema_version"`
	Name              string `json:"name"`
	FrozenAt          string `json:"frozen_at"`
	CorpusName        string `json:"corpus_name"`
	Split             string `json:"split"`
	ExpectedCases     int    `json:"expected_cases"`
	RankingLockSHA256 string `json:"ranking_lock_sha256"`
	Minimum           struct {
		HitAt3  float64 `json:"hit_at_3"`
		HitAt10 float64 `json:"hit_at_10"`
		MRRAt10 float64 `json:"mrr_at_10"`
	} `json:"minimum"`
	Maximum struct {
		Errors             int   `json:"errors"`
		ClientP95LatencyMS int64 `json:"client_p95_latency_ms"`
	} `json:"maximum"`
	FailurePolicy string `json:"failure_policy"`
}

type gateResult struct {
	SchemaVersion     int                      `json:"schema_version"`
	Name              string                   `json:"name"`
	RunID             string                   `json:"run_id"`
	CheckedAt         time.Time                `json:"checked_at"`
	ArtifactSHA256    string                   `json:"artifact_sha256"`
	RankingLockSHA256 string                   `json:"ranking_lock_sha256"`
	CodeCommit        string                   `json:"code_commit"`
	ImageDigest       string                   `json:"image_digest"`
	Observed          searcheval.BucketSummary `json:"observed"`
	ClientP95MS       int64                    `json:"client_p95_latency_ms"`
	Verdict           string                   `json:"verdict"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "evalgate: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("evalgate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	gatePath := flags.String("gate", "scripts/searcheval/corpus/v2/release-gate.json", "frozen release gate")
	artifactPath := flags.String("artifact", "", "immutable JSONL evaluation artifact")
	lockPath := flags.String("ranking-lock", "searchindex/ranking.lock.json", "frozen ranking lock")
	resultPath := flags.String("out", "", "exclusive 0600 gate-result JSON path")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *artifactPath == "" || *resultPath == "" {
		return errors.New("invalid invocation")
	}
	gate, err := readGate(*gatePath)
	if err != nil {
		return err
	}
	lockDigest, err := fileDigest(*lockPath)
	if err != nil {
		return err
	}
	if lockDigest != gate.RankingLockSHA256 {
		return errors.New("ranking lock does not match the frozen release gate")
	}
	metadata, summary, cases, artifactDigest, err := readArtifact(*artifactPath)
	if err != nil {
		return err
	}
	result := gateResult{
		SchemaVersion: 1, Name: gate.Name + "-result", RunID: metadata.RunID, CheckedAt: time.Now().UTC(),
		ArtifactSHA256: artifactDigest, RankingLockSHA256: lockDigest, CodeCommit: metadata.CodeCommit,
		ImageDigest: metadata.ImageDigest, Observed: summary.Overall, ClientP95MS: summary.ClientP95LatencyMS, Verdict: "pass",
	}
	if metadata.SchemaVersion != gate.SchemaVersion || metadata.CorpusName != gate.CorpusName || metadata.FrozenAt != gate.FrozenAt || metadata.Split != gate.Split ||
		metadata.CodeCommit == "" || metadata.ImageDigest == "" || cases != gate.ExpectedCases || summary.Overall.Total != gate.ExpectedCases ||
		summary.Overall.Errors > gate.Maximum.Errors || summary.Overall.HitAt3 < gate.Minimum.HitAt3 || summary.Overall.HitAt10 < gate.Minimum.HitAt10 ||
		summary.Overall.MRRAt10 < gate.Minimum.MRRAt10 || summary.ClientP95LatencyMS > gate.Maximum.ClientP95LatencyMS {
		result.Verdict = "fail"
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := writeExclusive(*resultPath, encoded); err != nil {
		return err
	}
	if _, err := output.Write(encoded); err != nil {
		return err
	}
	if result.Verdict != "pass" {
		return errors.New("frozen Search release gate failed; do not tune against heldout")
	}
	return nil
}

func readGate(path string) (releaseGate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return releaseGate{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var gate releaseGate
	if err := decoder.Decode(&gate); err != nil {
		return releaseGate{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return releaseGate{}, errors.New("release gate contains trailing JSON")
	}
	if gate.SchemaVersion != 1 || gate.Name == "" || gate.CorpusName == "" || gate.Split != "heldout" || gate.ExpectedCases < 1 || len(gate.RankingLockSHA256) != sha256.Size*2 || gate.FailurePolicy == "" {
		return releaseGate{}, errors.New("release gate is invalid")
	}
	if _, err := hex.DecodeString(gate.RankingLockSHA256); err != nil {
		return releaseGate{}, errors.New("release gate ranking digest is invalid")
	}
	return gate, nil
}

func readArtifact(path string) (searcheval.RunMetadata, searcheval.Summary, int, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return searcheval.RunMetadata{}, searcheval.Summary{}, 0, "", err
	}
	defer file.Close()
	hash := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(file, hash))
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	var metadata searcheval.RunMetadata
	var summary searcheval.Summary
	cases := 0
	line := 0
	for scanner.Scan() {
		line++
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			return metadata, summary, 0, "", fmt.Errorf("artifact line %d: %w", line, err)
		}
		switch envelope.Type {
		case "run":
			if line != 1 || metadata.Type != "" || json.Unmarshal(scanner.Bytes(), &metadata) != nil {
				return metadata, summary, 0, "", errors.New("artifact run metadata is invalid")
			}
		case "case":
			if metadata.Type == "" || summary.Type != "" {
				return metadata, summary, 0, "", errors.New("artifact case ordering is invalid")
			}
			cases++
		case "summary":
			if summary.Type != "" || json.Unmarshal(scanner.Bytes(), &summary) != nil {
				return metadata, summary, 0, "", errors.New("artifact summary is invalid")
			}
		default:
			return metadata, summary, 0, "", fmt.Errorf("artifact line %d has unknown type", line)
		}
	}
	if err := scanner.Err(); err != nil {
		return metadata, summary, 0, "", err
	}
	if metadata.Type != "run" || summary.Type != "summary" || line != cases+2 {
		return metadata, summary, 0, "", errors.New("artifact is incomplete")
	}
	return metadata, summary, cases, hex.EncodeToString(hash.Sum(nil)), nil
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func writeExclusive(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
