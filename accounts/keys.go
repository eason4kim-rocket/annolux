package accounts

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// CreateAPIKey returns the plaintext key exactly once. Only its HMAC digest
// and a short display prefix are retained.
func (store *Store) CreateAPIKey(ctx context.Context, accountID, name string) (APIKey, string, error) {
	name = strings.TrimSpace(name)
	if ctx == nil || strings.TrimSpace(accountID) == "" || name == "" || len(name) > 80 {
		return APIKey{}, "", ErrInvalidInput
	}
	secret, err := store.randomToken(32)
	if err != nil {
		return APIKey{}, "", err
	}
	plaintext := APIKeyPrefix + secret
	prefixLength := len(APIKeyPrefix) + 8
	if prefixLength > len(plaintext) {
		prefixLength = len(plaintext)
	}
	keyID, err := store.newID("key_")
	if err != nil {
		return APIKey{}, "", err
	}
	now := store.now().UTC().Round(0)
	key := APIKey{ID: keyID, AccountID: accountID, Prefix: plaintext[:prefixLength], Name: name, CreatedAt: now}
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
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE account_id=? AND revoked_at IS NULL`, accountID).Scan(&active); err != nil {
			return err
		}
		if active >= MaximumAPIKeys {
			return ErrMaximumAPIKeys
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO api_keys(id,account_id,prefix,key_hash,name,created_at,last_used_at,revoked_at) VALUES(?,?,?,?,?,?,NULL,NULL)`,
			key.ID, key.AccountID, key.Prefix, store.digest(plaintext), key.Name, now.Unix())
		return translateConstraint(err)
	})
	if err != nil {
		return APIKey{}, "", err
	}
	return key, plaintext, nil
}

func (store *Store) AuthenticateAPIKey(ctx context.Context, plaintext string) (AuthenticatedKey, error) {
	if store == nil || store.db == nil || ctx == nil || !strings.HasPrefix(plaintext, APIKeyPrefix) || len(plaintext) < len(APIKeyPrefix)+32 || len(plaintext) > 128 {
		return AuthenticatedKey{}, ErrNotFound
	}
	var authenticated AuthenticatedKey
	err := store.db.QueryRowContext(ctx, `
		SELECT k.id,k.account_id,a.status,k.prefix
		FROM api_keys k JOIN accounts a ON a.id=k.account_id
		WHERE k.key_hash=? AND k.revoked_at IS NULL`, store.digest(plaintext)).
		Scan(&authenticated.KeyID, &authenticated.AccountID, &authenticated.Status, &authenticated.Prefix)
	if isNoRows(err) {
		return AuthenticatedKey{}, ErrNotFound
	}
	if err != nil {
		return AuthenticatedKey{}, err
	}
	if authenticated.Status != AccountActive {
		return AuthenticatedKey{}, ErrSuspended
	}
	// Authentication does not block on a best-effort last-used write. Keeping
	// this out of the security decision prevents database telemetry from
	// changing whether a valid key is accepted.
	store.writeMu.Lock()
	_, _ = store.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at=? WHERE id=? AND (last_used_at IS NULL OR last_used_at<?)`,
		store.now().UTC().Unix(), authenticated.KeyID, store.now().UTC().Add(-time.Minute).Unix())
	store.writeMu.Unlock()
	return authenticated, nil
}

func (store *Store) ListAPIKeys(ctx context.Context, accountID string) ([]APIKey, error) {
	if store == nil || store.db == nil || ctx == nil || strings.TrimSpace(accountID) == "" {
		return nil, ErrInvalidInput
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id,account_id,prefix,name,created_at,last_used_at,revoked_at FROM api_keys WHERE account_id=? ORDER BY created_at,id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]APIKey, 0, MaximumAPIKeys)
	for rows.Next() {
		var key APIKey
		var createdAt int64
		var lastUsedAt, revokedAt sql.NullInt64
		if err := rows.Scan(&key.ID, &key.AccountID, &key.Prefix, &key.Name, &createdAt, &lastUsedAt, &revokedAt); err != nil {
			return nil, err
		}
		key.CreatedAt = unixTime(createdAt)
		key.LastUsedAt, key.RevokedAt = nullableTime(lastUsedAt), nullableTime(revokedAt)
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (store *Store) RevokeAPIKey(ctx context.Context, accountID, keyID string) error {
	if ctx == nil || strings.TrimSpace(accountID) == "" || strings.TrimSpace(keyID) == "" {
		return ErrInvalidInput
	}
	now := store.now().UTC().Unix()
	return store.withWriteTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE api_keys SET revoked_at=? WHERE id=? AND account_id=? AND revoked_at IS NULL`, now, keyID, accountID)
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
