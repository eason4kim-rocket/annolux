package accounts

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// CreateSession returns a bearer cookie value and its deterministic CSRF token.
// The database retains only an HMAC digest of the bearer value.
func (store *Store) CreateSession(ctx context.Context, accountID string, ttl time.Duration) (Session, string, string, error) {
	if ctx == nil || strings.TrimSpace(accountID) == "" || ttl <= 0 || ttl > 90*24*time.Hour {
		return Session{}, "", "", ErrInvalidInput
	}
	token, err := store.randomToken(32)
	if err != nil {
		return Session{}, "", "", err
	}
	sessionID, err := store.newID("sess_")
	if err != nil {
		return Session{}, "", "", err
	}
	now := store.now().UTC().Round(0)
	session := Session{ID: sessionID, AccountID: accountID, CreatedAt: now, ExpiresAt: now.Add(ttl)}
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
		_, err := tx.ExecContext(ctx, `INSERT INTO sessions(id,token_hash,account_id,expires_at,created_at,last_seen_at,revoked_at) VALUES(?,?,?,?,?,?,NULL)`,
			session.ID, store.digest(token), session.AccountID, session.ExpiresAt.Unix(), now.Unix(), now.Unix())
		return translateConstraint(err)
	})
	if err != nil {
		return Session{}, "", "", err
	}
	return session, token, store.csrfToken(token), nil
}

func (store *Store) AuthenticateSession(ctx context.Context, token string) (Session, string, error) {
	if store == nil || store.db == nil || ctx == nil || len(token) < 32 || len(token) > 128 {
		return Session{}, "", ErrNotFound
	}
	var session Session
	var expiresAt, createdAt int64
	var status AccountStatus
	err := store.db.QueryRowContext(ctx, `
		SELECT s.id,s.account_id,s.expires_at,s.created_at,a.status
		FROM sessions s JOIN accounts a ON a.id=s.account_id
		WHERE s.token_hash=? AND s.revoked_at IS NULL`, store.digest(token)).
		Scan(&session.ID, &session.AccountID, &expiresAt, &createdAt, &status)
	if isNoRows(err) {
		return Session{}, "", ErrNotFound
	}
	if err != nil {
		return Session{}, "", err
	}
	session.ExpiresAt, session.CreatedAt = unixTime(expiresAt), unixTime(createdAt)
	if !session.ExpiresAt.After(store.now().UTC()) {
		return Session{}, "", ErrExpired
	}
	if status != AccountActive {
		return Session{}, "", ErrSuspended
	}
	return session, store.csrfToken(token), nil
}

func (store *Store) RevokeSession(ctx context.Context, token string) error {
	if ctx == nil || len(token) < 32 || len(token) > 128 {
		return ErrInvalidInput
	}
	now := store.now().UTC().Unix()
	return store.withWriteTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, now, store.digest(token))
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (store *Store) ValidateCSRF(sessionToken, supplied string) bool {
	if store == nil || sessionToken == "" || supplied == "" {
		return false
	}
	return constantTimeEqual([]byte(store.csrfToken(sessionToken)), []byte(supplied))
}
