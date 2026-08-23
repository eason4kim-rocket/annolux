package quality

const ErrCodeContentUnusable = "CONTENT_UNUSABLE"

type Status string

const (
	StatusGood     Status = "good"
	StatusDegraded Status = "degraded"
	StatusUnusable Status = "unusable"
)

type Reason string

const (
	ReasonMissingBody       Reason = "missing_body"
	ReasonEmptyContent      Reason = "empty_content"
	ReasonLoadingPage       Reason = "loading_page"
	ReasonErrorPage         Reason = "error_page"
	ReasonChallengePage     Reason = "challenge_page"
	ReasonLowContentQuality Reason = "low_content_quality"
)

type AttemptOutcome string

const (
	AttemptSelected AttemptOutcome = "selected"
	AttemptRejected AttemptOutcome = "rejected"
	AttemptFailed   AttemptOutcome = "failed"
)

type Attempt struct {
	Engine     string         `json:"engine"`
	Outcome    AttemptOutcome `json:"outcome"`
	Reason     Reason         `json:"reason,omitempty"`
	DurationMs int64          `json:"duration_ms"`
}

type Info struct {
	Score           float64   `json:"score"`
	Status          Status    `json:"status"`
	Warnings        []Reason  `json:"warnings"`
	ExtractModeUsed string    `json:"extract_mode_used,omitempty"`
	FetchAttempts   []Attempt `json:"fetch_attempts,omitempty"`
}
