package accounts

type schemaMigration struct {
	version    int
	statements []string
}

var schemaMigrations = []schemaMigration{{
	version: 1,
	statements: []string{
		`CREATE TABLE IF NOT EXISTS accounts (
		id TEXT PRIMARY KEY,
		status TEXT NOT NULL CHECK(status IN ('active','suspended','closed')),
		terms_version TEXT NOT NULL,
		refund_version TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	) STRICT`,
		`CREATE TABLE IF NOT EXISTS oauth_identities (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		provider TEXT NOT NULL CHECK(provider IN ('google','github')),
		subject TEXT NOT NULL,
		email TEXT NOT NULL DEFAULT '',
		display_name TEXT NOT NULL DEFAULT '',
		avatar_url TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		last_login_at INTEGER NOT NULL,
		UNIQUE(provider, subject)
	) STRICT`,
		`CREATE INDEX IF NOT EXISTS oauth_identities_account_idx ON oauth_identities(account_id)`,
		`CREATE TABLE IF NOT EXISTS oauth_challenges (
		state_hash BLOB PRIMARY KEY,
		provider TEXT NOT NULL CHECK(provider IN ('google','github')),
		account_id TEXT REFERENCES accounts(id) ON DELETE CASCADE,
		code_verifier TEXT NOT NULL,
		nonce TEXT NOT NULL,
		return_path TEXT NOT NULL,
		browser_hash BLOB NOT NULL,
		expires_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	) STRICT`,
		`CREATE INDEX IF NOT EXISTS oauth_challenges_expiry_idx ON oauth_challenges(expires_at)`,
		`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		token_hash BLOB NOT NULL UNIQUE,
		account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		expires_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		last_seen_at INTEGER NOT NULL,
		revoked_at INTEGER
	) STRICT`,
		`CREATE INDEX IF NOT EXISTS sessions_account_idx ON sessions(account_id)`,
		`CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expires_at)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		prefix TEXT NOT NULL,
		key_hash BLOB NOT NULL UNIQUE,
		name TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		last_used_at INTEGER,
		revoked_at INTEGER
	) STRICT`,
		`CREATE INDEX IF NOT EXISTS api_keys_account_idx ON api_keys(account_id, revoked_at)`,
		`CREATE TABLE IF NOT EXISTS credit_grants (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		source TEXT NOT NULL CHECK(source IN ('signup','subscription','admin')),
		original_units INTEGER NOT NULL CHECK(original_units > 0),
		remaining_units INTEGER NOT NULL CHECK(remaining_units >= 0 AND remaining_units <= original_units),
		starts_at INTEGER NOT NULL,
		expires_at INTEGER,
		external_ref TEXT NOT NULL UNIQUE,
		created_at INTEGER NOT NULL
	) STRICT`,
		`CREATE INDEX IF NOT EXISTS credit_grants_spend_idx ON credit_grants(account_id, starts_at, expires_at, remaining_units)`,
		`CREATE TABLE IF NOT EXISTS credit_reservations (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		api_key_id TEXT REFERENCES api_keys(id),
		request_id TEXT NOT NULL UNIQUE,
		units INTEGER NOT NULL CHECK(units > 0),
		state TEXT NOT NULL CHECK(state IN ('pending','committed','released')),
		created_at INTEGER NOT NULL,
		settled_at INTEGER
	) STRICT`,
		`CREATE INDEX IF NOT EXISTS credit_reservations_account_idx ON credit_reservations(account_id, state, created_at)`,
		`CREATE TABLE IF NOT EXISTS credit_reservation_allocations (
		reservation_id TEXT NOT NULL REFERENCES credit_reservations(id) ON DELETE CASCADE,
		grant_id TEXT NOT NULL REFERENCES credit_grants(id),
		units INTEGER NOT NULL CHECK(units > 0),
		PRIMARY KEY(reservation_id, grant_id)
	) STRICT`,
		`CREATE TABLE IF NOT EXISTS credit_ledger (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		kind TEXT NOT NULL CHECK(kind IN ('grant','usage','expiry','revocation','adjustment')),
		units INTEGER NOT NULL,
		grant_id TEXT REFERENCES credit_grants(id),
		reservation_id TEXT REFERENCES credit_reservations(id),
		external_ref TEXT NOT NULL UNIQUE,
		created_at INTEGER NOT NULL
	) STRICT`,
		`CREATE INDEX IF NOT EXISTS credit_ledger_account_idx ON credit_ledger(account_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS checkout_intents (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		plan TEXT NOT NULL CHECK(plan IN ('pro','scale')),
		terms_version TEXT NOT NULL,
		refund_version TEXT NOT NULL,
		state TEXT NOT NULL CHECK(state IN ('pending','completed','expired','cancelled')),
		paddle_transaction_id TEXT NOT NULL DEFAULT '',
		expires_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	) STRICT`,
		`CREATE INDEX IF NOT EXISTS checkout_intents_account_idx ON checkout_intents(account_id, state)`,
		`CREATE TABLE IF NOT EXISTS paddle_customers (
		paddle_customer_id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	) STRICT`,
		`CREATE TABLE IF NOT EXISTS paddle_subscriptions (
		paddle_subscription_id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		plan TEXT NOT NULL CHECK(plan IN ('pro','scale')),
		status TEXT NOT NULL,
		current_period_start INTEGER NOT NULL,
		current_period_end INTEGER NOT NULL,
		cancel_at_period_end INTEGER NOT NULL CHECK(cancel_at_period_end IN (0,1)),
		is_current INTEGER NOT NULL DEFAULT 1 CHECK(is_current IN (0,1)),
		last_event_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	) STRICT`,
		`CREATE UNIQUE INDEX IF NOT EXISTS paddle_subscriptions_current_account_idx ON paddle_subscriptions(account_id) WHERE is_current=1`,
		`CREATE TABLE IF NOT EXISTS paddle_transactions (
		paddle_transaction_id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL REFERENCES accounts(id),
		paddle_subscription_id TEXT NOT NULL DEFAULT '',
		plan TEXT NOT NULL CHECK(plan IN ('pro','scale')),
		status TEXT NOT NULL,
		period_start INTEGER NOT NULL,
		period_end INTEGER NOT NULL,
		credit_grant_id TEXT REFERENCES credit_grants(id),
		last_event_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	) STRICT`,
		`CREATE TABLE IF NOT EXISTS paddle_events (
		event_id TEXT PRIMARY KEY,
		event_type TEXT NOT NULL,
		occurred_at INTEGER NOT NULL,
		payload BLOB NOT NULL,
		state TEXT NOT NULL CHECK(state IN ('pending','processed','failed','ignored')),
		attempts INTEGER NOT NULL DEFAULT 0,
		last_error TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	) STRICT`,
		`CREATE INDEX IF NOT EXISTS paddle_events_reconcile_idx ON paddle_events(state, occurred_at)`,
	},
}, {
	version: 2,
	statements: []string{
		`CREATE TABLE IF NOT EXISTS identity_tombstones (
			provider TEXT NOT NULL CHECK(provider IN ('google','github')),
			subject_hash BLOB NOT NULL,
			closed_at INTEGER NOT NULL,
			PRIMARY KEY(provider,subject_hash)
		) STRICT`,
	},
}, {
	version: 3,
	statements: []string{
		// Paddle timestamps include fractional seconds. Older schemas stored
		// them as Unix seconds, which let a delayed event in the same second
		// overwrite a newer subscription state. Keep the legacy second columns
		// for forward compatibility and add nanosecond ordering columns.
		`ALTER TABLE paddle_subscriptions ADD COLUMN last_event_at_ns INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE paddle_subscriptions ADD COLUMN last_event_key TEXT NOT NULL DEFAULT ''`,
		`UPDATE paddle_subscriptions SET last_event_at_ns=last_event_at*1000000000`,
		`UPDATE paddle_subscriptions SET is_current=0 WHERE status='canceled'`,
		`ALTER TABLE paddle_transactions ADD COLUMN last_event_at_ns INTEGER NOT NULL DEFAULT 0`,
		`UPDATE paddle_transactions SET last_event_at_ns=last_event_at*1000000000`,
		`ALTER TABLE paddle_events ADD COLUMN occurred_at_ns INTEGER NOT NULL DEFAULT 0`,
		`UPDATE paddle_events SET occurred_at_ns=occurred_at*1000000000`,
		`CREATE INDEX IF NOT EXISTS paddle_events_reconcile_ns_idx ON paddle_events(state,occurred_at_ns,event_id)`,
	},
}}
