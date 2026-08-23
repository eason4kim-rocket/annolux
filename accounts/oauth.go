package accounts

import (
	"context"
	"database/sql"
	"time"
)

// CreateOAuthChallenge stores one short-lived PKCE transaction. Raw state and
// browser-binding values are returned to the caller but retained only as HMAC
// digests. The code verifier is needed once for the server-side token exchange.
func (store *Store) CreateOAuthChallenge(ctx context.Context, provider, returnPath string, ttl time.Duration) (string, OAuthChallenge, error) {
	return store.CreateOAuthChallengeForAccount(ctx, provider, returnPath, "", ttl)
}

func (store *Store) CreateOAuthChallengeForAccount(ctx context.Context, provider, returnPath, accountID string, ttl time.Duration) (string, OAuthChallenge, error) {
	if ctx == nil || (provider != "google" && provider != "github") || ttl <= 0 || ttl > time.Hour || !validReturnPath(returnPath) {
		return "", OAuthChallenge{}, ErrInvalidInput
	}
	state, err := store.randomToken(32)
	if err != nil {
		return "", OAuthChallenge{}, err
	}
	verifier, err := store.randomToken(48)
	if err != nil {
		return "", OAuthChallenge{}, err
	}
	nonce, err := store.randomToken(32)
	if err != nil {
		return "", OAuthChallenge{}, err
	}
	browserCookie, err := store.randomToken(32)
	if err != nil {
		return "", OAuthChallenge{}, err
	}
	now := store.now().UTC().Round(0)
	challenge := OAuthChallenge{
		Provider: provider, AccountID: accountID, CodeVerifier: verifier, Nonce: nonce, ReturnPath: returnPath,
		BrowserCookie: browserCookie, ExpiresAt: now.Add(ttl),
	}
	err = store.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_challenges WHERE expires_at<=?`, now.Unix()); err != nil {
			return err
		}
		var accountValue any
		if accountID != "" {
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
			accountValue = accountID
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO oauth_challenges(state_hash,provider,account_id,code_verifier,nonce,return_path,browser_hash,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			store.digest(state), provider, accountValue, verifier, nonce, returnPath, store.digest(browserCookie), challenge.ExpiresAt.Unix(), now.Unix())
		return translateConstraint(err)
	})
	if err != nil {
		return "", OAuthChallenge{}, err
	}
	return state, challenge, nil
}

// ConsumeOAuthChallenge atomically deletes state before returning it, making
// callback replay impossible even when the provider token exchange later fails.
func (store *Store) ConsumeOAuthChallenge(ctx context.Context, state, browserCookie string) (OAuthChallenge, error) {
	if ctx == nil || len(state) < 32 || len(state) > 128 || len(browserCookie) < 32 || len(browserCookie) > 128 {
		return OAuthChallenge{}, ErrInvalidInput
	}
	now := store.now().UTC().Round(0)
	var challenge OAuthChallenge
	err := store.withWriteTx(ctx, func(tx *sql.Tx) error {
		var browserHash []byte
		var expiresAt int64
		var accountID sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT provider,account_id,code_verifier,nonce,return_path,browser_hash,expires_at FROM oauth_challenges WHERE state_hash=?`, store.digest(state)).
			Scan(&challenge.Provider, &accountID, &challenge.CodeVerifier, &challenge.Nonce, &challenge.ReturnPath, &browserHash, &expiresAt)
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_challenges WHERE state_hash=?`, store.digest(state)); err != nil {
			return err
		}
		challenge.ExpiresAt = unixTime(expiresAt)
		challenge.AccountID = nullableString(accountID)
		if !challenge.ExpiresAt.After(now) {
			return ErrExpired
		}
		if !constantTimeEqual(browserHash, store.digest(browserCookie)) {
			return ErrNotFound
		}
		return nil
	})
	return challenge, err
}

func validReturnPath(value string) bool {
	// OAuth is only used to enter or link an account. Keeping this list small
	// prevents an authorization callback from becoming an arbitrary redirect
	// primitive, including through encoded path or query-string ambiguity.
	switch value {
	case "", "/account", "/zh/account", "/billing/checkout", "/zh/billing/checkout",
		"/playground", "/zh/playground":
		return true
	default:
		return false
	}
}
