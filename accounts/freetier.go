package accounts

import (
	"context"
	"errors"
	"time"
)

// monthlyFreeWindow returns the current UTC calendar-month window and the
// idempotency key for an account's free-tier grant. The reference is keyed by
// YYYYMM so a second call in the same month is a no-op, while the next month
// grants a fresh batch. Non-stacking falls out of the window: each grant expires
// when the next month begins, so the spend-time filter drops the previous one.
func monthlyFreeWindow(accountID string, now time.Time) (monthStart, nextMonthStart time.Time, externalRef string) {
	utc := now.UTC()
	monthStart = time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
	nextMonthStart = monthStart.AddDate(0, 1, 0)
	externalRef = "free-monthly:" + accountID + ":" + monthStart.Format("200601")
	return monthStart, nextMonthStart, externalRef
}

// EnsureMonthlyFreeCredits grants the current calendar month's free-tier credits
// to an account when they have not been granted yet. It is idempotent per account
// per month (via the grant's external_ref) and cheap enough to call on every
// request: an indexed existence check short-circuits the common case, so only the
// first request of a new month touches the write path.
//
// Suspended or vanished accounts, and concurrent grants for the same month, are
// tolerated silently — the caller's downstream credit reservation is the
// authoritative "do you have credits" check, so this top-up never fails a request.
func (store *Store) EnsureMonthlyFreeCredits(ctx context.Context, accountID string) (CreditGrant, bool, error) {
	if store == nil || store.db == nil || ctx == nil || accountID == "" {
		return CreditGrant{}, false, ErrInvalidInput
	}
	monthStart, nextMonthStart, externalRef := monthlyFreeWindow(accountID, store.now())

	var exists int
	err := store.db.QueryRowContext(ctx, `SELECT 1 FROM credit_grants WHERE external_ref=?`, externalRef).Scan(&exists)
	if err == nil {
		return CreditGrant{}, false, nil
	}
	if !isNoRows(err) {
		return CreditGrant{}, false, err
	}

	grant, created, err := store.GrantCredits(ctx, accountID, "signup", FreeMonthlyCredits, monthStart, &nextMonthStart, externalRef)
	if err != nil {
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrSuspended) || errors.Is(err, ErrNotFound) {
			return CreditGrant{}, false, nil
		}
		return CreditGrant{}, false, err
	}
	return grant, created, nil
}
