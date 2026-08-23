// Package quality provides deterministic, side-effect-free content quality
// assessment for fetched HTML and cleaner output.
package quality

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/eason4kim-rocket/annolux/models"
	"golang.org/x/net/html"
)

const (
	// MinUsableScore is the inclusive lower bound for a candidate that may be
	// returned successfully. Lower-scoring candidates must trigger escalation.
	MinUsableScore = 0.70

	// GoodScore is the inclusive lower bound for a candidate with no quality
	// warning. Scores in [MinUsableScore, GoodScore) are degraded but usable.
	GoodScore = 0.85
)

// Candidate contains the outputs needed to judge one fetch/clean attempt.
// RawHTML is assessed for document completeness and substituted system pages;
// CleanedContent is scored as the content that would actually be returned.
type Candidate struct {
	RawHTML        string
	CleanedContent string
	ExtractMode    string
}

// Assessment is the internal decision returned for one candidate. Info is the
// additive public quality block. Reason is empty for accepted candidates and a
// stable machine-readable rejection reason otherwise. HardFailure distinguishes
// structural/substitution failures from a merely low content score.
type Assessment struct {
	Info        Info
	Reason      Reason
	HardFailure bool
}

// Usable reports whether an ordered fetch service may select this candidate.
func (a Assessment) Usable() bool {
	return !a.HardFailure && a.Info.Score >= MinUsableScore
}

// AsFetchAttempt converts a quality decision into the public attempt record an
// ordered engine can append to QualityInfo.FetchAttempts.
func (a Assessment) AsFetchAttempt(engine string, duration time.Duration) Attempt {
	attempt := Attempt{
		Engine:     engine,
		Outcome:    AttemptSelected,
		DurationMs: duration.Milliseconds(),
	}
	if !a.Usable() {
		attempt.Outcome = AttemptRejected
		attempt.Reason = a.RejectionReason()
	}
	return attempt
}

// RejectionReason returns the stable reason to record for an unusable
// candidate. It is empty for candidates that may be selected.
func (a Assessment) RejectionReason() Reason {
	if a.Usable() {
		return ""
	}
	if a.Reason != "" {
		return a.Reason
	}
	return ReasonLowContentQuality
}

// ContentUnusableError returns the terminal error an ordered service should
// use after all candidates have been rejected. It returns nil for a usable
// candidate so callers cannot accidentally turn a selected page into an error.
func (a Assessment) ContentUnusableError() *models.ServiceError {
	if a.Usable() {
		return nil
	}
	return NewContentUnusableError(a.RejectionReason())
}

// NewContentUnusableError creates the stable public terminal error for a
// quality-gated fetch sequence.
func NewContentUnusableError(reason Reason) *models.ServiceError {
	message := "fetched page content is unusable"
	if reason != "" {
		message = fmt.Sprintf("%s: %s", message, reason)
	}
	return models.NewServiceError(ErrCodeContentUnusable, message, nil)
}

// StatusForScore applies the public 0.70/0.85 boundary contract.
func StatusForScore(score float64) Status {
	switch {
	case score >= GoodScore:
		return StatusGood
	case score >= MinUsableScore:
		return StatusDegraded
	default:
		return StatusUnusable
	}
}

// EvaluateCandidate applies raw-document hard gates before assessing the
// cleaned content. It is the primary entry point for ordered fetch engines.
func EvaluateCandidate(candidate Candidate) Assessment {
	if reason := EvaluateRawDocument(candidate.RawHTML); reason != "" {
		return hardFailure(reason, candidate.ExtractMode)
	}

	return EvaluateCleanedContent(candidate.CleanedContent, candidate.ExtractMode)
}

// EvaluateRawDocument applies the hard document gates that are available
// before cleaning. An empty reason means the document is complete enough to
// enter the cleaning pipeline; a non-empty reason should reject the fetch
// candidate immediately.
func EvaluateRawDocument(rawHTML string) Reason {
	hasBody, bodyText := documentBodyText(rawHTML)
	if !hasBody {
		return ReasonMissingBody
	}
	if bodyText == "" {
		return ReasonEmptyContent
	}
	return substitutedPageReason(rawHTML, bodyText)
}

// EvaluateCleanedContent assesses content after readability/pruning/raw format
// conversion. Cleaner fallback selection can call this function without
// fabricating a complete HTML document.
func EvaluateCleanedContent(content, extractMode string) Assessment {
	text := visibleFragmentText(content)
	if text == "" {
		return hardFailure(ReasonEmptyContent, extractMode)
	}
	if reason := substitutedPageReason(content, text); reason != "" {
		return hardFailure(reason, extractMode)
	}

	score := cleanedContentScore(text)
	status := StatusForScore(score)
	warnings := make([]Reason, 0, 1)
	assessment := Assessment{
		Info: Info{
			Score:           score,
			Status:          status,
			Warnings:        warnings,
			ExtractModeUsed: extractMode,
		},
	}
	if status != StatusGood {
		assessment.Info.Warnings = append(assessment.Info.Warnings, ReasonLowContentQuality)
	}
	if status == StatusUnusable {
		assessment.Reason = ReasonLowContentQuality
	}
	return assessment
}

func hardFailure(reason Reason, extractMode string) Assessment {
	return Assessment{
		Info: Info{
			Score:           0,
			Status:          StatusUnusable,
			Warnings:        []Reason{reason},
			ExtractModeUsed: extractMode,
		},
		Reason:      reason,
		HardFailure: true,
	}
}

// cleanedContentScore intentionally uses only the returned text. Length sets a
// conservative base score while the informative-rune ratio prevents markup,
// punctuation, or repeated UI glyphs from passing on byte count alone.
func cleanedContentScore(text string) float64 {
	runeCount := utf8.RuneCountInString(text)
	var score float64
	switch {
	case runeCount >= 1000:
		score = 0.95
	case runeCount >= 400:
		score = 0.90
	case runeCount >= 180:
		score = 0.85
	case runeCount >= 80:
		score = 0.75
	case runeCount >= 40:
		score = 0.70
	case runeCount >= 16:
		score = 0.60
	default:
		score = 0.40
	}

	informative := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			informative++
		}
	}
	ratio := 0.0
	if runeCount > 0 {
		ratio = float64(informative) / float64(runeCount)
	}
	switch {
	case ratio >= 0.55:
		score += 0.05
	case ratio < 0.30:
		score -= 0.15
	}

	score = math.Max(0, math.Min(1, score))
	return math.Round(score*100) / 100
}

func documentBodyText(rawHTML string) (bool, string) {
	if strings.TrimSpace(rawHTML) == "" {
		return false, ""
	}

	tokenizer := html.NewTokenizer(strings.NewReader(rawHTML))
	var text strings.Builder
	inBody := false
	hasBody := false
	ignoredDepth := 0

	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.ErrorToken:
			return hasBody, collapseWhitespace(text.String())
		case html.StartTagToken:
			name, _ := tokenizer.TagName()
			tag := string(name)
			if tag == "body" {
				hasBody = true
				inBody = true
				continue
			}
			if inBody {
				if ignoredDepth > 0 {
					ignoredDepth++
				} else if ignoredTextTag(tag) {
					ignoredDepth = 1
				}
			}
		case html.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			if string(name) == "body" {
				hasBody = true
			}
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			tag := string(name)
			if inBody && ignoredDepth > 0 {
				ignoredDepth--
			}
			if tag == "body" {
				inBody = false
				ignoredDepth = 0
			}
		case html.TextToken:
			if inBody && ignoredDepth == 0 {
				text.Write(tokenizer.Text())
				text.WriteByte(' ')
			}
		}
	}
}

func visibleFragmentText(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}

	tokenizer := html.NewTokenizer(strings.NewReader(content))
	var text strings.Builder
	ignoredDepth := 0
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.ErrorToken:
			return collapseWhitespace(text.String())
		case html.StartTagToken:
			name, _ := tokenizer.TagName()
			if ignoredDepth > 0 {
				ignoredDepth++
			} else if ignoredTextTag(string(name)) {
				ignoredDepth = 1
			}
		case html.EndTagToken:
			if ignoredDepth > 0 {
				ignoredDepth--
			}
		case html.TextToken:
			if ignoredDepth == 0 {
				text.Write(tokenizer.Text())
				text.WriteByte(' ')
			}
		}
	}
}

func ignoredTextTag(tag string) bool {
	switch tag {
	case "script", "style", "noscript", "template", "svg":
		return true
	default:
		return false
	}
}

func substitutedPageReason(raw, visibleText string) Reason {
	lowerRaw := strings.ToLower(raw)
	lowerText := strings.ToLower(collapseWhitespace(visibleText))
	textRunes := utf8.RuneCountInString(lowerText)

	if textRunes <= 2000 && containsAny(lowerRaw,
		"cf-chl-",
		"challenge-platform",
		"__cf_chl",
		"_pxcaptcha",
		"hcaptcha-box",
	) {
		return ReasonChallengePage
	}
	if textRunes <= 1200 && containsAny(lowerText,
		"checking your browser",
		"verify you are human",
		"complete the security check",
		"attention required",
		"enable javascript and cookies to continue",
		"captcha challenge",
	) {
		return ReasonChallengePage
	}

	if containsAny(lowerRaw,
		"chrome-error://chromewebdata/",
		"id=\"main-frame-error\"",
		"id='main-frame-error'",
		"about:neterror",
	) {
		return ReasonErrorPage
	}
	if textRunes <= 1200 && strings.Contains(lowerRaw, "data-error-code=") {
		return ReasonErrorPage
	}
	if textRunes <= 1200 && containsAny(lowerText,
		"this site can't be reached",
		"this site can’t be reached",
		"err_name_not_resolved",
		"internal server error",
		"502 bad gateway",
		"503 service unavailable",
		"404 not found",
		"page not found",
		"aw, snap",
	) {
		return ReasonErrorPage
	}

	if textRunes <= 500 {
		trimmed := strings.Trim(lowerText, " \t\r\n.!…")
		if looksLikeLoadingText(trimmed) {
			return ReasonLoadingPage
		}
		if textRunes <= 100 && containsAny(lowerRaw,
			"aria-busy=\"true\"",
			"aria-busy='true'",
		) {
			return ReasonLoadingPage
		}
	}

	return ""
}

func looksLikeLoadingText(text string) bool {
	switch text {
	case "loading", "loading content", "please wait", "正在加载", "加载中", "正在载入", "请稍候":
		return true
	}
	wordCount := len(strings.Fields(text))
	if wordCount <= 12 && strings.HasPrefix(text, "please wait") {
		return true
	}
	if wordCount <= 6 && strings.HasPrefix(text, "loading content") {
		return true
	}
	if utf8.RuneCountInString(text) <= 20 && containsAny(text, "正在加载", "加载中", "正在载入", "请稍候") {
		return true
	}
	return false
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
