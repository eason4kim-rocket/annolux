// Package accounts owns Annolux account identity, API keys, sessions, and the
// append-only credit ledger. It is deliberately independent from the search
// index database so billing data can be backed up and restored separately.
package accounts

import (
	"errors"
	"time"
)

const (
	// FreeMonthlyCredits is the free tier: this amount is granted at signup and
	// refreshed at the start of each calendar month. Unused credits do not roll
	// over — each month's grant expires when the next month begins.
	FreeMonthlyCredits int64 = 1_000
	MaximumAPIKeys           = 3
	APIKeyPrefix             = "ann_live_"
	DefaultSessionTTL        = 30 * 24 * time.Hour
	DefaultOAuthTTL          = 10 * time.Minute
)

var (
	ErrInvalidInput        = errors.New("accounts: invalid input")
	ErrNotFound            = errors.New("accounts: not found")
	ErrConflict            = errors.New("accounts: conflict")
	ErrSuspended           = errors.New("accounts: account is suspended")
	ErrClosed              = errors.New("accounts: account is closed")
	ErrInsufficientCredits = errors.New("accounts: insufficient credits")
	ErrMaximumAPIKeys      = errors.New("accounts: maximum active API keys reached")
	ErrExpired             = errors.New("accounts: expired")
	ErrAlreadyUsed         = errors.New("accounts: already used")
)

type AccountStatus string

const (
	AccountActive    AccountStatus = "active"
	AccountSuspended AccountStatus = "suspended"
	AccountClosed    AccountStatus = "closed"
)

type Account struct {
	ID            string        `json:"id"`
	Status        AccountStatus `json:"status"`
	TermsVersion  string        `json:"terms_version"`
	RefundVersion string        `json:"refund_version"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type Identity struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"-"`
	Provider    string    `json:"provider"`
	Subject     string    `json:"subject"`
	Email       string    `json:"email,omitempty"`
	DisplayName string    `json:"display_name,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
}

type APIKey struct {
	ID         string     `json:"id"`
	AccountID  string     `json:"-"`
	Prefix     string     `json:"prefix"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

type AuthenticatedKey struct {
	KeyID     string
	AccountID string
	Status    AccountStatus
	Prefix    string
}

type Session struct {
	ID        string
	AccountID string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type OAuthChallenge struct {
	Provider      string
	AccountID     string
	CodeVerifier  string
	Nonce         string
	ReturnPath    string
	BrowserCookie string
	ExpiresAt     time.Time
}

type CreditGrant struct {
	ID          string     `json:"id"`
	AccountID   string     `json:"-"`
	Source      string     `json:"source"`
	Original    int64      `json:"original"`
	Remaining   int64      `json:"remaining"`
	StartsAt    time.Time  `json:"starts_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	ExternalRef string     `json:"-"`
	CreatedAt   time.Time  `json:"created_at"`
}

type CreditReservation struct {
	ID        string
	AccountID string
	APIKeyID  string
	RequestID string
	Units     int64
	State     string
	CreatedAt time.Time
	SettledAt *time.Time
}

type CreditSummary struct {
	Available int64         `json:"available"`
	Granted   int64         `json:"granted"`
	Used      int64         `json:"used"`
	Expired   int64         `json:"expired"`
	Revoked   int64         `json:"revoked"`
	Reserved  int64         `json:"reserved"`
	Grants    []CreditGrant `json:"grants"`
}

type LedgerEntry struct {
	ID            string
	AccountID     string
	Kind          string
	Units         int64
	GrantID       string
	ReservationID string
	ExternalRef   string
	CreatedAt     time.Time
}

// OperationalMetrics contains aggregate launch signals only. It deliberately
// excludes identities, email addresses, API-key prefixes, queries, domains,
// IP addresses, and request IDs so it is safe for an operator status check.
type OperationalMetrics struct {
	GeneratedAt               time.Time `json:"generated_at"`
	Accounts                  int64     `json:"accounts"`
	ActiveAccounts            int64     `json:"active_accounts"`
	SuspendedAccounts         int64     `json:"suspended_accounts"`
	ClosedAccounts            int64     `json:"closed_accounts"`
	ActiveAPIKeys             int64     `json:"active_api_keys"`
	AccountsWithSearch        int64     `json:"accounts_with_search"`
	AccountsWithRepeatDayUse  int64     `json:"accounts_with_repeat_day_use"`
	CommittedSearches         int64     `json:"committed_searches"`
	CommittedSearches24Hours  int64     `json:"committed_searches_24_hours"`
	PendingCreditReservations int64     `json:"pending_credit_reservations"`
	CurrentSubscriptions      int64     `json:"current_subscriptions"`
	PendingCheckoutIntents    int64     `json:"pending_checkout_intents"`
	PendingPaddleEvents       int64     `json:"pending_paddle_events"`
	FailedPaddleEvents        int64     `json:"failed_paddle_events"`
}

type PaddleEvent struct {
	EventID    string
	EventType  string
	OccurredAt time.Time
	Payload    []byte
	State      string
	Attempts   int
	LastError  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type CheckoutIntent struct {
	ID                  string
	AccountID           string
	Plan                string
	TermsVersion        string
	RefundVersion       string
	State               string
	PaddleTransactionID string
	ExpiresAt           time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type PaddleSubscription struct {
	ID                string
	AccountID         string
	Plan              string
	Status            string
	PeriodStart       time.Time
	PeriodEnd         time.Time
	CancelAtPeriodEnd bool
	Current           bool
	LastEventAt       time.Time
}

type CompletedTransaction struct {
	TransactionID    string
	SubscriptionID   string
	CustomerID       string
	CheckoutIntentID string
	Origin           string
	Plan             string
	PeriodStart      time.Time
	PeriodEnd        time.Time
	OccurredAt       time.Time
}

type SubscriptionUpdate struct {
	SubscriptionID    string
	CustomerID        string
	CheckoutIntentID  string
	EventKey          string
	Plan              string
	Status            string
	PeriodStart       time.Time
	PeriodEnd         time.Time
	CancelAtPeriodEnd bool
	OccurredAt        time.Time
}
