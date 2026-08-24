package accounts

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func openTestStore(t *testing.T, clock *time.Time) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "accounts.db"), Config{
		HMACSecret: []byte("0123456789abcdef0123456789abcdef"),
		Now:        func() time.Time { return *clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestOpenMigratesLegacyPaddleEventOrderingToNanoseconds(t *testing.T) {
	clock := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "accounts.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range schemaMigrations[:2] {
		for _, statement := range migration.statements {
			if _, err := legacy.Exec(statement); err != nil {
				t.Fatalf("legacy migration %d: %v", migration.version, err)
			}
		}
		if _, err := legacy.Exec(`INSERT INTO schema_migrations(version,applied_at) VALUES(?,?)`, migration.version, clock.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	legacySecond := clock.Add(-time.Hour).Unix()
	if _, err := legacy.Exec(`INSERT INTO accounts(id,status,terms_version,refund_version,created_at,updated_at) VALUES('acc_legacy','active','terms','refund',?,?)`, legacySecond, legacySecond); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO paddle_subscriptions(paddle_subscription_id,account_id,plan,status,current_period_start,current_period_end,cancel_at_period_end,is_current,last_event_at,created_at,updated_at) VALUES('sub_legacy','acc_legacy','pro','canceled',?,?,0,1,?,?,?)`, legacySecond, legacySecond+3600, legacySecond, legacySecond, legacySecond); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO paddle_transactions(paddle_transaction_id,account_id,paddle_subscription_id,plan,status,period_start,period_end,credit_grant_id,last_event_at,created_at,updated_at) VALUES('txn_legacy','acc_legacy','sub_legacy','pro','completed',?,?,NULL,?,?,?)`, legacySecond, legacySecond+3600, legacySecond, legacySecond, legacySecond); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO paddle_events(event_id,event_type,occurred_at,payload,state,attempts,last_error,created_at,updated_at) VALUES('evt_legacy','subscription.updated',?,?,'processed',1,'',?,?)`, legacySecond, []byte(`{}`), legacySecond, legacySecond); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(context.Background(), path, Config{
		HMACSecret: []byte("0123456789abcdef0123456789abcdef"),
		Now:        func() time.Time { return clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	want := legacySecond * int64(time.Second)
	var subscriptionNS, transactionNS, eventNS, current int64
	var eventKey string
	if err := store.db.QueryRow(`SELECT last_event_at_ns,last_event_key,is_current FROM paddle_subscriptions WHERE paddle_subscription_id='sub_legacy'`).Scan(&subscriptionNS, &eventKey, &current); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT last_event_at_ns FROM paddle_transactions WHERE paddle_transaction_id='txn_legacy'`).Scan(&transactionNS); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT occurred_at_ns FROM paddle_events WHERE event_id='evt_legacy'`).Scan(&eventNS); err != nil {
		t.Fatal(err)
	}
	if subscriptionNS != want || transactionNS != want || eventNS != want || eventKey != "" || current != 0 {
		t.Fatalf("migrated ordering = subscription %d/%q/current=%d transaction %d event %d, want %d", subscriptionNS, eventKey, current, transactionNS, eventNS, want)
	}
}

func TestOpenRecordsForwardOnlySchemaVersion(t *testing.T) {
	clock := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "accounts.db")
	store, err := Open(context.Background(), path, Config{
		HMACSecret: []byte("0123456789abcdef0123456789abcdef"),
		Now:        func() time.Time { return clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	var version, appliedAt int64
	if err := store.db.QueryRow(`SELECT version,applied_at FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version, &appliedAt); err != nil {
		t.Fatal(err)
	}
	if version != 3 || appliedAt != clock.Unix() {
		t.Fatalf("migration = version %d at %d", version, appliedAt)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), path, Config{
		HMACSecret: []byte("0123456789abcdef0123456789abcdef"),
		Now:        func() time.Time { return clock.Add(time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	var count int
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("migration count = %d, %v", count, err)
	}
}

func TestOpenCreatesPrivateDatabaseAndRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "accounts.db")
	store, err := Open(context.Background(), path, Config{HMACSecret: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("accounts.db permissions = %04o, want 0600", got)
	}

	link := filepath.Join(directory, "accounts-link.db")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), link, Config{HMACSecret: []byte("0123456789abcdef0123456789abcdef")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Open(symlink) error = %v, want ErrInvalidInput", err)
	}
}

func createTestAccount(t *testing.T, store *Store, suffix string) Account {
	t.Helper()
	account, created, err := store.LoginIdentity(context.Background(), IdentityInput{
		Provider: "google", Subject: "google-" + suffix, Email: suffix + "@example.com", DisplayName: "Test " + suffix,
	})
	if err != nil || !created {
		t.Fatalf("LoginIdentity() = %#v, %t, %v", account, created, err)
	}
	if account.TermsVersion != DefaultTermsVersion || account.RefundVersion != DefaultRefundVersion {
		t.Fatalf("policy versions = %q %q", account.TermsVersion, account.RefundVersion)
	}
	return account
}

func TestIdentityLoginSignupGrantAndNoEmailMerge(t *testing.T) {
	clock := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	ctx := context.Background()
	first, created, err := store.LoginIdentity(ctx, IdentityInput{Provider: "google", Subject: "g-1", Email: "same@example.com", DisplayName: "First"})
	if err != nil || !created {
		t.Fatalf("first login = %#v, %t, %v", first, created, err)
	}
	again, created, err := store.LoginIdentity(ctx, IdentityInput{Provider: "google", Subject: "g-1", Email: "new@example.com", DisplayName: "Updated"})
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("repeat login = %#v, %t, %v", again, created, err)
	}
	second, created, err := store.LoginIdentity(ctx, IdentityInput{Provider: "github", Subject: "99", Email: "same@example.com"})
	if err != nil || !created || second.ID == first.ID {
		t.Fatalf("email merge occurred: %#v, %t, %v", second, created, err)
	}
	for _, accountID := range []string{first.ID, second.ID} {
		summary, err := store.CreditSummary(ctx, accountID)
		if err != nil || summary.Available != FreeMonthlyCredits || summary.Granted != FreeMonthlyCredits || len(summary.Grants) != 1 || summary.Grants[0].ExpiresAt == nil || !summary.Grants[0].ExpiresAt.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("summary(%s) = %#v, %v", accountID, summary, err)
		}
	}
}

func TestLinkedIdentityDoesNotDuplicateSignupGrant(t *testing.T) {
	clock := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "link")
	identity, err := store.LinkIdentity(context.Background(), account.ID, IdentityInput{Provider: "github", Subject: "123", Email: "link@example.com"})
	if err != nil || identity.AccountID != account.ID {
		t.Fatalf("LinkIdentity() = %#v, %v", identity, err)
	}
	summary, err := store.CreditSummary(context.Background(), account.ID)
	if err != nil || summary.Granted != FreeMonthlyCredits || len(summary.Grants) != 1 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
}

func TestCloseAccountErasesOAuthProfileAndTombstonesSignupIdentity(t *testing.T) {
	clock := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	google := IdentityInput{Provider: "google", Subject: "close-google-sub", Email: "person@example.com", DisplayName: "Private Name", AvatarURL: "https://example.com/avatar.png"}
	account, created, err := store.LoginIdentity(context.Background(), google)
	if err != nil || !created {
		t.Fatalf("signup = %#v, %t, %v", account, created, err)
	}
	github := IdentityInput{Provider: "github", Subject: "424242", Email: "same@example.com", DisplayName: "Private Name"}
	if _, err := store.LinkIdentity(context.Background(), account.ID, github); err != nil {
		t.Fatal(err)
	}
	_, secret, err := store.CreateAPIKey(context.Background(), account.ID, "close-test")
	if err != nil {
		t.Fatal(err)
	}
	_, sessionToken, _, err := store.CreateSession(context.Background(), account.ID, DefaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CloseAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseAccount(context.Background(), account.ID); err != nil {
		t.Fatalf("idempotent close = %v", err)
	}
	closed, err := store.GetAccount(context.Background(), account.ID)
	if err != nil || closed.Status != AccountClosed {
		t.Fatalf("closed account = %#v, %v", closed, err)
	}
	identities, err := store.ListIdentities(context.Background(), account.ID)
	if err != nil || len(identities) != 0 {
		t.Fatalf("identities = %#v, %v", identities, err)
	}
	if _, err := store.AuthenticateAPIKey(context.Background(), secret); !errors.Is(err, ErrNotFound) {
		t.Fatalf("closed API key = %v", err)
	}
	if _, _, err := store.AuthenticateSession(context.Background(), sessionToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("closed session = %v", err)
	}
	for _, identity := range []IdentityInput{google, github} {
		if _, _, err := store.LoginIdentity(context.Background(), identity); !errors.Is(err, ErrClosed) {
			t.Fatalf("closed identity %s login = %v", identity.Provider, err)
		}
	}
	if err := store.SetAccountStatus(context.Background(), account.ID, AccountActive); !errors.Is(err, ErrClosed) {
		t.Fatalf("reopened closed account = %v", err)
	}
	var accountsCount, grantsCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&accountsCount); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM credit_grants`).Scan(&grantsCount); err != nil {
		t.Fatal(err)
	}
	if accountsCount != 1 || grantsCount != 1 {
		t.Fatalf("accounts=%d grants=%d", accountsCount, grantsCount)
	}
}

func TestAPIKeyShownOnceLimitAuthenticationAndRevocation(t *testing.T) {
	clock := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "keys")
	ctx := context.Background()
	var first APIKey
	var plaintext string
	for index := 0; index < MaximumAPIKeys; index++ {
		key, secret, err := store.CreateAPIKey(ctx, account.ID, "key")
		if err != nil || !stringsHasPrefix(secret, APIKeyPrefix) || secret == key.Prefix {
			t.Fatalf("CreateAPIKey(%d) = %#v, %q, %v", index, key, secret, err)
		}
		if index == 0 {
			first, plaintext = key, secret
		}
	}
	if _, _, err := store.CreateAPIKey(ctx, account.ID, "overflow"); !errors.Is(err, ErrMaximumAPIKeys) {
		t.Fatalf("overflow error = %v", err)
	}
	authenticated, err := store.AuthenticateAPIKey(ctx, plaintext)
	if err != nil || authenticated.AccountID != account.ID || authenticated.KeyID != first.ID {
		t.Fatalf("AuthenticateAPIKey() = %#v, %v", authenticated, err)
	}
	if err := store.RevokeAPIKey(ctx, account.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateAPIKey(ctx, plaintext); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked key authentication = %v", err)
	}
	if _, _, err := store.CreateAPIKey(ctx, account.ID, "replacement"); err != nil {
		t.Fatalf("replacement key = %v", err)
	}
}

func TestCreditReservationCommitReleaseExpiryAndOrdering(t *testing.T) {
	clock := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "credits")
	key, _, err := store.CreateAPIKey(context.Background(), account.ID, "primary")
	if err != nil {
		t.Fatal(err)
	}
	// The paid grant expires before the monthly free grant's month boundary, so
	// spending draws from the paid grant first and only it expires mid-test.
	expires := clock.Add(10 * 24 * time.Hour)
	paid, created, err := store.GrantCredits(context.Background(), account.ID, "subscription", 3, clock, &expires, "txn:1")
	if err != nil || !created {
		t.Fatalf("GrantCredits() = %#v, %t, %v", paid, created, err)
	}
	reservation, remaining, err := store.ReserveCredits(context.Background(), account.ID, key.ID, "request-1", 1)
	if err != nil || remaining != FreeMonthlyCredits+2 {
		t.Fatalf("ReserveCredits() = %#v, %d, %v", reservation, remaining, err)
	}
	summary, err := store.CreditSummary(context.Background(), account.ID)
	paidRemaining := int64(-1)
	for _, grant := range summary.Grants {
		if grant.ID == paid.ID {
			paidRemaining = grant.Remaining
		}
	}
	if err != nil || summary.Reserved != 1 || paidRemaining != 2 {
		t.Fatalf("reserved summary = %#v, %v", summary, err)
	}
	remaining, err = store.CommitReservation(context.Background(), reservation.ID)
	if err != nil || remaining != FreeMonthlyCredits+2 {
		t.Fatalf("CommitReservation() = %d, %v", remaining, err)
	}
	second, remaining, err := store.ReserveCredits(context.Background(), account.ID, key.ID, "request-2", 1)
	if err != nil || remaining != FreeMonthlyCredits+1 {
		t.Fatalf("second reserve = %#v, %d, %v", second, remaining, err)
	}
	remaining, err = store.ReleaseReservation(context.Background(), second.ID)
	if err != nil || remaining != FreeMonthlyCredits+2 {
		t.Fatalf("ReleaseReservation() = %d, %v", remaining, err)
	}
	clock = expires.Add(time.Second)
	expired, err := store.ExpireCredits(context.Background())
	if err != nil || expired != 1 {
		t.Fatalf("ExpireCredits() = %d, %v", expired, err)
	}
	summary, err = store.CreditSummary(context.Background(), account.ID)
	if err != nil || summary.Available != FreeMonthlyCredits || summary.Used != 1 || summary.Expired != 2 {
		t.Fatalf("final summary = %#v, %v", summary, err)
	}
}

func TestConcurrentReservationsNeverDriveBalanceNegative(t *testing.T) {
	clock := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "concurrent")
	key, _, err := store.CreateAPIKey(context.Background(), account.ID, "primary")
	if err != nil {
		t.Fatal(err)
	}
	const attempts = int(FreeMonthlyCredits) + 20
	var group sync.WaitGroup
	var mutex sync.Mutex
	successes := 0
	for index := 0; index < attempts; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			reservation, _, err := store.ReserveCredits(context.Background(), account.ID, key.ID, fmtInt(index), 1)
			if err == nil {
				if _, err := store.CommitReservation(context.Background(), reservation.ID); err != nil {
					t.Errorf("commit %d: %v", index, err)
					return
				}
				mutex.Lock()
				successes++
				mutex.Unlock()
				return
			}
			if !errors.Is(err, ErrInsufficientCredits) {
				t.Errorf("reserve %d: %v", index, err)
			}
		}(index)
	}
	group.Wait()
	summary, err := store.CreditSummary(context.Background(), account.ID)
	if err != nil || successes != int(FreeMonthlyCredits) || summary.Available != 0 || summary.Used != FreeMonthlyCredits {
		t.Fatalf("successes=%d summary=%#v err=%v", successes, summary, err)
	}
}

func TestStaleReservationRecoveryAfterCrash(t *testing.T) {
	clock := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "crash-recovery")
	key, _, err := store.CreateAPIKey(context.Background(), account.ID, "crash")
	if err != nil {
		t.Fatal(err)
	}
	reservation, remaining, err := store.ReserveCredits(context.Background(), account.ID, key.ID, "crashed-request", 1)
	if err != nil || reservation.State != "pending" || remaining != FreeMonthlyCredits-1 {
		t.Fatalf("reservation = %#v, remaining=%d, err=%v", reservation, remaining, err)
	}
	clock = clock.Add(6 * time.Minute)
	released, err := store.ReleaseStaleReservations(context.Background(), 5*time.Minute)
	if err != nil || released != 1 {
		t.Fatalf("recovery = %d, %v", released, err)
	}
	summary, err := store.CreditSummary(context.Background(), account.ID)
	if err != nil || summary.Available != FreeMonthlyCredits || summary.Reserved != 0 || summary.Used != 0 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
	if released, err = store.ReleaseStaleReservations(context.Background(), 5*time.Minute); err != nil || released != 0 {
		t.Fatalf("repeat recovery = %d, %v", released, err)
	}
}

func TestSessionCSRFAndOAuthChallengeReplay(t *testing.T) {
	clock := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "session")
	session, token, csrf, err := store.CreateSession(context.Background(), account.ID, DefaultSessionTTL)
	if err != nil || token == "" || csrf == "" || !store.ValidateCSRF(token, csrf) || store.ValidateCSRF(token, csrf+"x") {
		t.Fatalf("CreateSession() = %#v, %q, %q, %v", session, token, csrf, err)
	}
	authenticated, derived, err := store.AuthenticateSession(context.Background(), token)
	if err != nil || authenticated.AccountID != account.ID || derived != csrf {
		t.Fatalf("AuthenticateSession() = %#v, %q, %v", authenticated, derived, err)
	}
	state, challenge, err := store.CreateOAuthChallenge(context.Background(), "google", "/account", DefaultOAuthTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeOAuthChallenge(context.Background(), state, "wrong-browser-cookie-value-that-is-long-enough"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong browser consume = %v", err)
	}
	consumed, err := store.ConsumeOAuthChallenge(context.Background(), state, challenge.BrowserCookie)
	if err != nil || consumed.CodeVerifier != challenge.CodeVerifier || consumed.Nonce != challenge.Nonce {
		t.Fatalf("ConsumeOAuthChallenge() = %#v, %v", consumed, err)
	}
	if _, err := store.ConsumeOAuthChallenge(context.Background(), state, challenge.BrowserCookie); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replay error = %v", err)
	}
	if err := store.RevokeSession(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AuthenticateSession(context.Background(), token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked session = %v", err)
	}
}

func TestRevokedKeyCannotCommitAnInFlightReservation(t *testing.T) {
	clock := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "in-flight-revoke")
	key, _, err := store.CreateAPIKey(context.Background(), account.ID, "primary")
	if err != nil {
		t.Fatal(err)
	}
	reservation, _, err := store.ReserveCredits(context.Background(), account.ID, key.ID, "in-flight-revoke", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeAPIKey(context.Background(), account.ID, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitReservation(context.Background(), reservation.ID); !errors.Is(err, ErrSuspended) {
		t.Fatalf("commit after key revocation = %v", err)
	}
	if _, err := store.ReleaseReservation(context.Background(), reservation.ID); err != nil {
		t.Fatal(err)
	}
	summary, err := store.CreditSummary(context.Background(), account.ID)
	if err != nil || summary.Available != FreeMonthlyCredits || summary.Used != 0 || summary.Reserved != 0 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
}

func stringsHasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

func fmtInt(value int) string {
	if value == 0 {
		return "request-0"
	}
	const digits = "0123456789"
	buffer := make([]byte, 0, 16)
	for value > 0 {
		buffer = append(buffer, digits[value%10])
		value /= 10
	}
	for left, right := 0, len(buffer)-1; left < right; left, right = left+1, right-1 {
		buffer[left], buffer[right] = buffer[right], buffer[left]
	}
	return "request-" + string(buffer)
}
