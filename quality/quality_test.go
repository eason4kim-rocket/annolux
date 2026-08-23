package quality

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStatusForScoreBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		score float64
		want  Status
	}{
		{name: "below zero", score: -0.1, want: StatusUnusable},
		{name: "below usable boundary", score: 0.699999, want: StatusUnusable},
		{name: "usable boundary inclusive", score: 0.70, want: StatusDegraded},
		{name: "below good boundary", score: 0.849999, want: StatusDegraded},
		{name: "good boundary inclusive", score: 0.85, want: StatusGood},
		{name: "perfect", score: 1, want: StatusGood},
		{name: "above one", score: 1.1, want: StatusGood},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StatusForScore(tt.score); got != tt.want {
				t.Fatalf("StatusForScore(%v) = %q, want %q", tt.score, got, tt.want)
			}
		})
	}
}

func TestEvaluateCandidateHardFailureMatrix(t *testing.T) {
	longCleaned := strings.Repeat("useful article content ", 100)
	tests := []struct {
		name    string
		rawHTML string
		cleaned string
		want    Reason
	}{
		{
			name:    "missing body",
			rawHTML: `<html><head><title>Head only</title></head></html>`,
			cleaned: longCleaned,
			want:    ReasonMissingBody,
		},
		{
			name:    "empty body ignores scripts styles and templates",
			rawHTML: `<html><body><script>document.write("late")</script><style>body{}</style><template>placeholder</template></body></html>`,
			cleaned: longCleaned,
			want:    ReasonEmptyContent,
		},
		{
			name:    "loading placeholder",
			rawHTML: `<html><body><main aria-busy="true">Loading content...</main></body></html>`,
			cleaned: `Loading content...`,
			want:    ReasonLoadingPage,
		},
		{
			name:    "browser error page",
			rawHTML: `<html><body><main id="main-frame-error">This site can't be reached ERR_NAME_NOT_RESOLVED</main></body></html>`,
			cleaned: `This site can't be reached ERR_NAME_NOT_RESOLVED`,
			want:    ReasonErrorPage,
		},
		{
			name:    "challenge substituted for target",
			rawHTML: `<html><body><div id="cf-chl-widget">Checking your browser before accessing the site</div></body></html>`,
			cleaned: `Checking your browser before accessing the site`,
			want:    ReasonChallengePage,
		},
		{
			name:    "cleaner returned empty content",
			rawHTML: `<html><body><article>The fetched document has a real body.</article></body></html>`,
			cleaned: `  `,
			want:    ReasonEmptyContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateCandidate(Candidate{
				RawHTML:        tt.rawHTML,
				CleanedContent: tt.cleaned,
				ExtractMode:    "readability",
			})
			if !got.HardFailure {
				t.Fatalf("HardFailure = false, want true: %#v", got)
			}
			if got.Usable() {
				t.Fatalf("Usable() = true, want false: %#v", got)
			}
			if got.Reason != tt.want || got.RejectionReason() != tt.want {
				t.Fatalf("reason = %q / %q, want %q", got.Reason, got.RejectionReason(), tt.want)
			}
			if got.Info.Score != 0 || got.Info.Status != StatusUnusable {
				t.Fatalf("quality = %#v, want zero/unusable", got.Info)
			}
			if len(got.Info.Warnings) != 1 || got.Info.Warnings[0] != tt.want {
				t.Fatalf("warnings = %#v, want [%q]", got.Info.Warnings, tt.want)
			}
			if got.Info.ExtractModeUsed != "readability" {
				t.Fatalf("extract_mode_used = %q, want readability", got.Info.ExtractModeUsed)
			}

			attempt := got.AsFetchAttempt("http", 820*time.Millisecond)
			if attempt.Outcome != AttemptRejected || attempt.Reason != tt.want || attempt.DurationMs != 820 {
				t.Fatalf("attempt = %#v", attempt)
			}

			qualityErr := got.ContentUnusableError()
			if qualityErr == nil || qualityErr.Code != ErrCodeContentUnusable {
				t.Fatalf("error = %#v, want code %q", qualityErr, ErrCodeContentUnusable)
			}
			if !strings.Contains(qualityErr.Message, string(tt.want)) {
				t.Fatalf("error message = %q, want reason %q", qualityErr.Message, tt.want)
			}
		})
	}
}

func TestEvaluateCleanedContentScoreMatrix(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantScore  float64
		wantStatus Status
		wantUsable bool
		wantHard   bool
	}{
		{
			name:       "substantial text is good",
			content:    strings.Repeat("useful ", 30),
			wantScore:  0.90,
			wantStatus: StatusGood,
			wantUsable: true,
		},
		{
			name:       "short but meaningful text is degraded",
			content:    strings.Repeat("useful ", 6),
			wantScore:  0.75,
			wantStatus: StatusDegraded,
			wantUsable: true,
		},
		{
			name:       "nonempty fragment below usable bar",
			content:    strings.Repeat("useful ", 3),
			wantScore:  0.65,
			wantStatus: StatusUnusable,
			wantUsable: false,
		},
		{
			name:       "markup volume does not count as content",
			content:    `<script>` + strings.Repeat("not visible ", 100) + `</script><style>body{color:red}</style>`,
			wantScore:  0,
			wantStatus: StatusUnusable,
			wantUsable: false,
			wantHard:   true,
		},
		{
			name:       "CJK text is informative",
			content:    strings.Repeat("可靠内容", 50),
			wantScore:  0.90,
			wantStatus: StatusGood,
			wantUsable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateCleanedContent(tt.content, "pruning")
			if got.Info.Score != tt.wantScore || got.Info.Status != tt.wantStatus {
				t.Fatalf("quality = %#v, want score=%v status=%q", got.Info, tt.wantScore, tt.wantStatus)
			}
			if got.Usable() != tt.wantUsable || got.HardFailure != tt.wantHard {
				t.Fatalf("Usable/HardFailure = %v/%v, want %v/%v", got.Usable(), got.HardFailure, tt.wantUsable, tt.wantHard)
			}
			if got.Info.ExtractModeUsed != "pruning" {
				t.Fatalf("extract_mode_used = %q, want pruning", got.Info.ExtractModeUsed)
			}
			if tt.wantStatus == StatusGood {
				if got.Info.Warnings == nil || len(got.Info.Warnings) != 0 {
					t.Fatalf("good warnings = %#v, want non-nil empty slice", got.Info.Warnings)
				}
			} else if len(got.Info.Warnings) == 0 {
				t.Fatal("non-good result must provide a warning")
			}
		})
	}
}

func TestEvaluateCleanedContentSubstitutedPageMatrix(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    Reason
	}{
		{
			name:    "loading output",
			content: "Please wait... Loading content",
			want:    ReasonLoadingPage,
		},
		{
			name:    "browser error output",
			content: "This site can’t be reached. ERR_NAME_NOT_RESOLVED",
			want:    ReasonErrorPage,
		},
		{
			name:    "challenge output",
			content: "Attention required: verify you are human to continue",
			want:    ReasonChallengePage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateCleanedContent(tt.content, "raw")
			if !got.HardFailure || got.Reason != tt.want || got.Usable() {
				t.Fatalf("assessment = %#v, want hard failure %q", got, tt.want)
			}
		})
	}
}

func TestEvaluateRawDocumentPassesCompleteBody(t *testing.T) {
	rawHTML := `<html><head><title>Title text is not body text</title></head><body><article>Actual article content.</article></body></html>`
	if reason := EvaluateRawDocument(rawHTML); reason != "" {
		t.Fatalf("EvaluateRawDocument() = %q, want pass", reason)
	}
}

func TestLoadingWordInRealContentIsNotAHardFailure(t *testing.T) {
	content := "Loading performance guide for frontend applications"
	rawHTML := `<html><body><article>` + content + `</article></body></html>`
	if reason := EvaluateRawDocument(rawHTML); reason != "" {
		t.Fatalf("EvaluateRawDocument() = %q, want no hard failure", reason)
	}
	got := EvaluateCleanedContent(content, "readability")
	if got.HardFailure || got.Reason == ReasonLoadingPage {
		t.Fatalf("real content misclassified as loading page: %#v", got)
	}
}

func TestEvaluateCandidateDoesNotRejectLongArticleQuotingChallengeLanguage(t *testing.T) {
	article := strings.Repeat("This article explains browser security checks in useful technical detail. ", 40) +
		"It quotes the phrase verify you are human while documenting challenge pages."
	rawHTML := `<html><body><article>` + article + `</article></body></html>`

	got := EvaluateCandidate(Candidate{RawHTML: rawHTML, CleanedContent: article, ExtractMode: "readability"})
	if !got.Usable() || got.HardFailure || got.Reason != "" {
		t.Fatalf("legitimate article rejected: %#v", got)
	}
	if got.ContentUnusableError() != nil {
		t.Fatal("usable result returned CONTENT_UNUSABLE")
	}
	attempt := got.AsFetchAttempt("rod", 2160*time.Millisecond)
	if attempt.Outcome != AttemptSelected || attempt.Reason != "" || attempt.DurationMs != 2160 {
		t.Fatalf("attempt = %#v", attempt)
	}
}

func TestQualityInfoPublicJSONContract(t *testing.T) {
	assessment := EvaluateCleanedContent(strings.Repeat("useful ", 30), "pruning")
	assessment.Info.FetchAttempts = []Attempt{
		{
			Engine:     "http",
			Outcome:    AttemptRejected,
			Reason:     ReasonMissingBody,
			DurationMs: 820,
		},
		{
			Engine:     "rod",
			Outcome:    AttemptSelected,
			DurationMs: 2160,
		},
	}

	encoded, err := json.Marshal(assessment.Info)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("Unmarshal() error = %v, JSON = %s", err, encoded)
	}
	for _, key := range []string{"score", "status", "warnings", "extract_mode_used", "fetch_attempts"} {
		if _, ok := document[key]; !ok {
			t.Fatalf("JSON %s missing key %q", encoded, key)
		}
	}
	warnings, ok := document["warnings"].([]any)
	if !ok || len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want []", document["warnings"])
	}
	attempts, ok := document["fetch_attempts"].([]any)
	if !ok || len(attempts) != 2 {
		t.Fatalf("fetch_attempts = %#v", document["fetch_attempts"])
	}
	selected := attempts[1].(map[string]any)
	if _, present := selected["reason"]; present {
		t.Fatalf("selected attempt unexpectedly has reason: %#v", selected)
	}
}

func TestNewContentUnusableErrorContract(t *testing.T) {
	err := NewContentUnusableError(ReasonLowContentQuality)
	if err.Code != "CONTENT_UNUSABLE" || err.ToDetail().Code != "CONTENT_UNUSABLE" {
		t.Fatalf("error = %#v, detail = %#v", err, err.ToDetail())
	}
}
