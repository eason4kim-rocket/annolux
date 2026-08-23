package accounts

import (
	"context"
	"testing"
	"time"
)

func TestMetricsAreAggregateAndCountRepeatDayUse(t *testing.T) {
	clock := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "metrics")
	key, _, err := store.CreateAPIKey(context.Background(), account.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	reservation, _, err := store.ReserveCredits(context.Background(), account.ID, key.ID, "request-one", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitReservation(context.Background(), reservation.ID); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(25 * time.Hour)
	reservation, _, err = store.ReserveCredits(context.Background(), account.ID, key.ID, "request-two", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitReservation(context.Background(), reservation.ID); err != nil {
		t.Fatal(err)
	}
	metrics, err := store.Metrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Accounts != 1 || metrics.ActiveAccounts != 1 || metrics.ActiveAPIKeys != 1 ||
		metrics.AccountsWithSearch != 1 || metrics.AccountsWithRepeatDayUse != 1 ||
		metrics.CommittedSearches != 2 || metrics.CommittedSearches24Hours != 1 || metrics.GeneratedAt != clock {
		t.Fatalf("metrics = %#v", metrics)
	}
}
