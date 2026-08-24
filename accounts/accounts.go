package accounts

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
)

type IdentityInput struct {
	Provider    string
	Subject     string
	Email       string
	DisplayName string
	AvatarURL   string
}

func validateIdentity(input IdentityInput) error {
	if (input.Provider != "google" && input.Provider != "github") ||
		strings.TrimSpace(input.Subject) == "" || len(input.Subject) > 512 ||
		len(input.Email) > 512 || len(input.DisplayName) > 512 || len(input.AvatarURL) > 4096 {
		return ErrInvalidInput
	}
	if input.AvatarURL != "" {
		parsed, err := url.Parse(input.AvatarURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return ErrInvalidInput
		}
	}
	return nil
}

// LoginIdentity returns the existing account for a provider subject or creates
// a new account seeded with the current month's free-tier credit grant (the
// recurring monthly refresh is handled by EnsureMonthlyFreeCredits). Email
// addresses are metadata only and are never used to merge identities.
func (store *Store) LoginIdentity(ctx context.Context, input IdentityInput) (Account, bool, error) {
	if err := validateIdentity(input); err != nil || ctx == nil {
		return Account{}, false, ErrInvalidInput
	}
	input.Subject = strings.TrimSpace(input.Subject)
	input.Email = strings.TrimSpace(input.Email)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	now := store.now().UTC().Round(0)
	accountID, err := store.newID("acct_")
	if err != nil {
		return Account{}, false, err
	}
	identityID, err := store.newID("idn_")
	if err != nil {
		return Account{}, false, err
	}
	grantID, err := store.newID("grant_")
	if err != nil {
		return Account{}, false, err
	}
	ledgerID, err := store.newID("led_")
	if err != nil {
		return Account{}, false, err
	}
	created := false
	var result Account
	err = store.withWriteTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `
			SELECT a.id, a.status, a.terms_version, a.refund_version, a.created_at, a.updated_at
			FROM oauth_identities i JOIN accounts a ON a.id=i.account_id
			WHERE i.provider=? AND i.subject=?`, input.Provider, input.Subject)
		var createdAt, updatedAt int64
		err := row.Scan(&result.ID, &result.Status, &result.TermsVersion, &result.RefundVersion, &createdAt, &updatedAt)
		if err == nil {
			result.CreatedAt, result.UpdatedAt = unixTime(createdAt), unixTime(updatedAt)
			if result.Status != AccountActive {
				return ErrSuspended
			}
			_, err = tx.ExecContext(ctx, `UPDATE oauth_identities SET email=?, display_name=?, avatar_url=?, last_login_at=? WHERE provider=? AND subject=?`,
				input.Email, input.DisplayName, input.AvatarURL, now.Unix(), input.Provider, input.Subject)
			return err
		}
		if !isNoRows(err) {
			return err
		}
		var tombstone int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM identity_tombstones WHERE provider=? AND subject_hash=?`, input.Provider, store.identityTombstoneDigest(input.Provider, input.Subject)).Scan(&tombstone)
		if err == nil {
			return ErrClosed
		}
		if !isNoRows(err) {
			return err
		}
		result = Account{ID: accountID, Status: AccountActive, TermsVersion: store.termsVersion, RefundVersion: store.refundVersion, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO accounts(id,status,terms_version,refund_version,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
			accountID, AccountActive, store.termsVersion, store.refundVersion, now.Unix(), now.Unix()); err != nil {
			return translateConstraint(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_identities(id,account_id,provider,subject,email,display_name,avatar_url,created_at,last_login_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			identityID, accountID, input.Provider, input.Subject, input.Email, input.DisplayName, input.AvatarURL, now.Unix(), now.Unix()); err != nil {
			return translateConstraint(err)
		}
		monthStart, nextMonthStart, externalRef := monthlyFreeWindow(accountID, now)
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_grants(id,account_id,source,original_units,remaining_units,starts_at,expires_at,external_ref,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			grantID, accountID, "signup", FreeMonthlyCredits, FreeMonthlyCredits, monthStart.Unix(), nextMonthStart.Unix(), externalRef, now.Unix()); err != nil {
			return translateConstraint(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(id,account_id,kind,units,grant_id,reservation_id,external_ref,created_at) VALUES(?,?,?,?,?,NULL,?,?)`,
			ledgerID, accountID, "grant", FreeMonthlyCredits, grantID, externalRef, now.Unix()); err != nil {
			return translateConstraint(err)
		}
		created = true
		return nil
	})
	return result, created, err
}

// LinkIdentity adds a second provider identity to an authenticated account.
// It never creates a credit grant.
func (store *Store) LinkIdentity(ctx context.Context, accountID string, input IdentityInput) (Identity, error) {
	if ctx == nil || strings.TrimSpace(accountID) == "" || validateIdentity(input) != nil {
		return Identity{}, ErrInvalidInput
	}
	identityID, err := store.newID("idn_")
	if err != nil {
		return Identity{}, err
	}
	now := store.now().UTC().Round(0)
	identity := Identity{
		ID: identityID, AccountID: accountID, Provider: input.Provider,
		Subject: strings.TrimSpace(input.Subject), Email: strings.TrimSpace(input.Email),
		DisplayName: strings.TrimSpace(input.DisplayName), AvatarURL: input.AvatarURL,
		CreatedAt: now, LastLoginAt: now,
	}
	err = store.withWriteTx(ctx, func(tx *sql.Tx) error {
		var status AccountStatus
		if err := tx.QueryRowContext(ctx, `SELECT status FROM accounts WHERE id=?`, accountID).Scan(&status); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if status != AccountActive {
			return ErrSuspended
		}
		var tombstone int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM identity_tombstones WHERE provider=? AND subject_hash=?`, input.Provider, store.identityTombstoneDigest(input.Provider, identity.Subject)).Scan(&tombstone)
		if err == nil {
			return ErrClosed
		}
		if !isNoRows(err) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO oauth_identities(id,account_id,provider,subject,email,display_name,avatar_url,created_at,last_login_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			identity.ID, identity.AccountID, identity.Provider, identity.Subject, identity.Email, identity.DisplayName, identity.AvatarURL, now.Unix(), now.Unix())
		return translateConstraint(err)
	})
	return identity, err
}

func (store *Store) GetAccount(ctx context.Context, accountID string) (Account, error) {
	if store == nil || store.db == nil || ctx == nil || strings.TrimSpace(accountID) == "" {
		return Account{}, ErrInvalidInput
	}
	var account Account
	var createdAt, updatedAt int64
	err := store.db.QueryRowContext(ctx, `SELECT id,status,terms_version,refund_version,created_at,updated_at FROM accounts WHERE id=?`, accountID).
		Scan(&account.ID, &account.Status, &account.TermsVersion, &account.RefundVersion, &createdAt, &updatedAt)
	if isNoRows(err) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	account.CreatedAt, account.UpdatedAt = unixTime(createdAt), unixTime(updatedAt)
	return account, nil
}

func (store *Store) ListIdentities(ctx context.Context, accountID string) ([]Identity, error) {
	if store == nil || store.db == nil || ctx == nil || strings.TrimSpace(accountID) == "" {
		return nil, ErrInvalidInput
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id,account_id,provider,subject,email,display_name,avatar_url,created_at,last_login_at FROM oauth_identities WHERE account_id=? ORDER BY created_at,id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	identities := make([]Identity, 0, 2)
	for rows.Next() {
		var identity Identity
		var createdAt, lastLoginAt int64
		if err := rows.Scan(&identity.ID, &identity.AccountID, &identity.Provider, &identity.Subject, &identity.Email, &identity.DisplayName, &identity.AvatarURL, &createdAt, &lastLoginAt); err != nil {
			return nil, err
		}
		identity.CreatedAt, identity.LastLoginAt = unixTime(createdAt), unixTime(lastLoginAt)
		identities = append(identities, identity)
	}
	return identities, rows.Err()
}

func (store *Store) AcceptPolicies(ctx context.Context, accountID, termsVersion, refundVersion string) error {
	if ctx == nil || strings.TrimSpace(accountID) == "" || strings.TrimSpace(termsVersion) == "" || strings.TrimSpace(refundVersion) == "" || len(termsVersion) > 64 || len(refundVersion) > 64 {
		return ErrInvalidInput
	}
	now := store.now().UTC().Unix()
	return store.withWriteTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE accounts SET terms_version=?, refund_version=?, updated_at=? WHERE id=? AND status='active'`, termsVersion, refundVersion, now, accountID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (store *Store) SetAccountStatus(ctx context.Context, accountID string, status AccountStatus) error {
	if ctx == nil || strings.TrimSpace(accountID) == "" || (status != AccountActive && status != AccountSuspended) {
		return ErrInvalidInput
	}
	now := store.now().UTC().Unix()
	return store.withWriteTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE accounts SET status=?,updated_at=? WHERE id=? AND status!='closed'`, status, now, accountID)
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			var existing AccountStatus
			if err := tx.QueryRowContext(ctx, `SELECT status FROM accounts WHERE id=?`, accountID).Scan(&existing); isNoRows(err) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
			return ErrClosed
		}
		if status != AccountActive {
			if _, err := tx.ExecContext(ctx, `UPDATE api_keys SET revoked_at=COALESCE(revoked_at,?) WHERE account_id=?`, now, accountID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,?) WHERE account_id=?`, now, accountID); err != nil {
				return err
			}
		}
		return nil
	})
}

// CloseAccount removes OAuth profile data and permanently tombstones every
// linked provider subject. Billing and credit records remain pseudonymous for
// fraud, tax, refund, and dispute obligations. An active paid subscription
// must be canceled before closure.
func (store *Store) CloseAccount(ctx context.Context, accountID string) error {
	if ctx == nil || strings.TrimSpace(accountID) == "" {
		return ErrInvalidInput
	}
	now := store.now().UTC().Unix()
	return store.withWriteTx(ctx, func(tx *sql.Tx) error {
		var status AccountStatus
		if err := tx.QueryRowContext(ctx, `SELECT status FROM accounts WHERE id=?`, accountID).Scan(&status); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if status == AccountClosed {
			return nil
		}
		var activeSubscriptions int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM paddle_subscriptions WHERE account_id=? AND is_current=1 AND status!='canceled'`, accountID).Scan(&activeSubscriptions); err != nil {
			return err
		}
		if activeSubscriptions != 0 {
			return ErrConflict
		}
		rows, err := tx.QueryContext(ctx, `SELECT provider,subject FROM oauth_identities WHERE account_id=?`, accountID)
		if err != nil {
			return err
		}
		type subject struct{ provider, value string }
		var subjects []subject
		for rows.Next() {
			var current subject
			if err := rows.Scan(&current.provider, &current.value); err != nil {
				_ = rows.Close()
				return err
			}
			subjects = append(subjects, current)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, current := range subjects {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO identity_tombstones(provider,subject_hash,closed_at) VALUES(?,?,?)`, current.provider, store.identityTombstoneDigest(current.provider, current.value), now); err != nil {
				return err
			}
		}
		for _, statement := range []string{`DELETE FROM oauth_identities WHERE account_id=?`, `DELETE FROM oauth_challenges WHERE account_id=?`} {
			if _, err := tx.ExecContext(ctx, statement, accountID); err != nil {
				return err
			}
		}
		for _, statement := range []string{
			`UPDATE api_keys SET revoked_at=COALESCE(revoked_at,?) WHERE account_id=?`,
			`UPDATE sessions SET revoked_at=COALESCE(revoked_at,?) WHERE account_id=?`,
			`UPDATE checkout_intents SET state='cancelled',updated_at=? WHERE account_id=? AND state='pending'`,
			`UPDATE accounts SET status='closed',updated_at=? WHERE id=?`,
		} {
			if _, err := tx.ExecContext(ctx, statement, now, accountID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (store *Store) identityTombstoneDigest(provider, subject string) []byte {
	return store.digest("identity-tombstone:" + provider + ":" + subject)
}

func scanAccount(row interface{ Scan(...any) error }) (Account, error) {
	var account Account
	var createdAt, updatedAt int64
	err := row.Scan(&account.ID, &account.Status, &account.TermsVersion, &account.RefundVersion, &createdAt, &updatedAt)
	if err != nil {
		return Account{}, err
	}
	account.CreatedAt, account.UpdatedAt = unixTime(createdAt), unixTime(updatedAt)
	return account, nil
}
