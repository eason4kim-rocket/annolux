package accounts

import (
	"context"
	"testing"
	"time"
)

func TestEnsureMonthlyFreeCreditsRefreshesEachMonthWithoutStacking(t *testing.T) {
	clock := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	ctx := context.Background()

	// createTestAccount signs up in August, which already seeds the August grant.
	account := createTestAccount(t, store, "freetier")

	// A same-month call is a no-op: the August grant already exists.
	if _, created, err := store.EnsureMonthlyFreeCredits(ctx, account.ID); err != nil || created {
		t.Fatalf("same-month ensure = created %t, %v", created, err)
	}
	summary, err := store.CreditSummary(ctx, account.ID)
	if err != nil || summary.Available != FreeMonthlyCredits || len(summary.Grants) != 1 {
		t.Fatalf("august summary = %#v, %v", summary, err)
	}
	if expiry := summary.Grants[0].ExpiresAt; expiry == nil || !expiry.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("august grant expiry = %v (want 2026-09-01)", expiry)
	}

	// Spend one credit so we can later prove the August grant does not carry over.
	key, _, err := store.CreateAPIKey(ctx, account.ID, "primary")
	if err != nil {
		t.Fatal(err)
	}
	reservation, _, err := store.ReserveCredits(ctx, account.ID, key.ID, "aug-use", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitReservation(ctx, reservation.ID); err != nil {
		t.Fatal(err)
	}

	// Next month: a fresh grant is issued and the August grant has expired.
	clock = time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	grant, created, err := store.EnsureMonthlyFreeCredits(ctx, account.ID)
	if err != nil || !created || grant.Original != FreeMonthlyCredits {
		t.Fatalf("september ensure = %#v, %t, %v", grant, created, err)
	}
	summary, err = store.CreditSummary(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Non-stacking: only September's 1,000 is available; August (spent 1, then
	// expired) does not fold its remaining 999 back into the balance.
	if summary.Available != FreeMonthlyCredits {
		t.Fatalf("september available = %d, want %d (no stacking)", summary.Available, FreeMonthlyCredits)
	}
	if len(summary.Grants) != 2 || summary.Granted != 2*FreeMonthlyCredits {
		t.Fatalf("september granted = %d across %d grants", summary.Granted, len(summary.Grants))
	}

	// A second September call is idempotent.
	if _, created, err := store.EnsureMonthlyFreeCredits(ctx, account.ID); err != nil || created {
		t.Fatalf("september repeat = created %t, %v", created, err)
	}
}

func TestEnsureMonthlyFreeCreditsSkipsSuspendedAccount(t *testing.T) {
	clock := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	ctx := context.Background()
	account := createTestAccount(t, store, "suspended")

	if err := store.SetAccountStatus(ctx, account.ID, AccountSuspended); err != nil {
		t.Fatal(err)
	}
	// Move to a new month with no existing grant, then ensure: a suspended
	// account is skipped silently — no error and no new grant.
	clock = time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	grant, created, err := store.EnsureMonthlyFreeCredits(ctx, account.ID)
	if err != nil || created {
		t.Fatalf("suspended ensure = %#v, %t, %v", grant, created, err)
	}
	summary, err := store.CreditSummary(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Grants) != 1 {
		t.Fatalf("suspended account grants = %d, want only the seeded August grant", len(summary.Grants))
	}
}
