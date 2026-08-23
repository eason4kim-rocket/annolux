package accounts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (store *Store) LedgerEntries(ctx context.Context, accountID string, limit int) ([]LedgerEntry, error) {
	if store == nil || store.db == nil || ctx == nil || accountID == "" || limit < 1 || limit > 1000 {
		return nil, ErrInvalidInput
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id,account_id,kind,units,COALESCE(grant_id,''),COALESCE(reservation_id,''),external_ref,created_at FROM credit_ledger WHERE account_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []LedgerEntry
	for rows.Next() {
		var entry LedgerEntry
		var createdAt int64
		if err := rows.Scan(&entry.ID, &entry.AccountID, &entry.Kind, &entry.Units, &entry.GrantID, &entry.ReservationID, &entry.ExternalRef, &createdAt); err != nil {
			return nil, err
		}
		entry.CreatedAt = unixTime(createdAt)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// Metrics returns PII-free aggregate launch signals for the local operator.
// Repeat-day use means at least one committed Search on two distinct UTC
// calendar days; it is intentionally stricter than two calls in one session.
func (store *Store) Metrics(ctx context.Context) (OperationalMetrics, error) {
	if store == nil || store.db == nil || ctx == nil {
		return OperationalMetrics{}, ErrInvalidInput
	}
	now := store.now().UTC().Round(0)
	metrics := OperationalMetrics{GeneratedAt: now}
	queries := []struct {
		query string
		args  []any
		into  []any
	}{
		{
			query: `SELECT COUNT(*),COALESCE(SUM(status='active'),0),COALESCE(SUM(status='suspended'),0),COALESCE(SUM(status='closed'),0) FROM accounts`,
			into:  []any{&metrics.Accounts, &metrics.ActiveAccounts, &metrics.SuspendedAccounts, &metrics.ClosedAccounts},
		},
		{query: `SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL`, into: []any{&metrics.ActiveAPIKeys}},
		{query: `SELECT COUNT(DISTINCT account_id) FROM credit_reservations WHERE state='committed'`, into: []any{&metrics.AccountsWithSearch}},
		{query: `SELECT COUNT(*) FROM (SELECT account_id FROM credit_reservations WHERE state='committed' GROUP BY account_id HAVING COUNT(DISTINCT date(created_at,'unixepoch'))>=2)`, into: []any{&metrics.AccountsWithRepeatDayUse}},
		{query: `SELECT COUNT(*) FROM credit_reservations WHERE state='committed'`, into: []any{&metrics.CommittedSearches}},
		{query: `SELECT COUNT(*) FROM credit_reservations WHERE state='committed' AND created_at>=?`, args: []any{now.Add(-24 * time.Hour).Unix()}, into: []any{&metrics.CommittedSearches24Hours}},
		{query: `SELECT COUNT(*) FROM credit_reservations WHERE state='pending'`, into: []any{&metrics.PendingCreditReservations}},
		{query: `SELECT COUNT(*) FROM paddle_subscriptions WHERE is_current=1`, into: []any{&metrics.CurrentSubscriptions}},
		{query: `SELECT COUNT(*) FROM checkout_intents WHERE state='pending' AND expires_at>?`, args: []any{now.Unix()}, into: []any{&metrics.PendingCheckoutIntents}},
		{query: `SELECT COUNT(*) FROM paddle_events WHERE state='pending'`, into: []any{&metrics.PendingPaddleEvents}},
		{query: `SELECT COUNT(*) FROM paddle_events WHERE state='failed'`, into: []any{&metrics.FailedPaddleEvents}},
	}
	for _, query := range queries {
		if err := store.db.QueryRowContext(ctx, query.query, query.args...).Scan(query.into...); err != nil {
			return OperationalMetrics{}, err
		}
	}
	return metrics, nil
}

func (store *Store) IntegrityCheck(ctx context.Context) error {
	if store == nil || store.db == nil || ctx == nil {
		return ErrInvalidInput
	}
	rows, err := store.db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			rows.Close()
			return err
		}
		if result != "ok" {
			rows.Close()
			return fmt.Errorf("accounts: SQLite integrity check: %s", result)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	checks := []struct {
		query   string
		message string
	}{
		{`SELECT COUNT(*) FROM credit_grants WHERE remaining_units<0 OR remaining_units>original_units`, "credit grant bounds are invalid"},
		{`SELECT COUNT(*) FROM credit_reservations r WHERE r.state='committed' AND NOT EXISTS(SELECT 1 FROM credit_ledger l WHERE l.reservation_id=r.id AND l.kind='usage')`, "committed reservation lacks usage entry"},
		{`SELECT COUNT(*) FROM credit_reservations r WHERE r.units!=(SELECT COALESCE(SUM(a.units),0) FROM credit_reservation_allocations a WHERE a.reservation_id=r.id)`, "reservation allocation total is invalid"},
		{`SELECT COUNT(*) FROM api_keys k LEFT JOIN accounts a ON a.id=k.account_id WHERE a.id IS NULL`, "API key has no account"},
	}
	for _, check := range checks {
		var count int
		if err := store.db.QueryRowContext(ctx, check.query).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("accounts: %s (%d rows)", check.message, count)
		}
	}
	return nil
}

// Snapshot creates a transactionally consistent SQLite copy using VACUUM
// INTO. Copying accounts.db directly while WAL is active is forbidden.
func (store *Store) Snapshot(ctx context.Context, destination string) error {
	if store == nil || store.db == nil || ctx == nil || strings.TrimSpace(destination) == "" {
		return ErrInvalidInput
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(absolute); err == nil {
		return fmt.Errorf("accounts: snapshot destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return err
	}
	store.writeMu.Lock()
	defer store.writeMu.Unlock()
	quoted := strings.ReplaceAll(absolute, "'", "''")
	if _, err := store.db.ExecContext(ctx, "VACUUM INTO '"+quoted+"'"); err != nil {
		return err
	}
	if err := os.Chmod(absolute, 0o600); err != nil {
		return err
	}
	return nil
}
