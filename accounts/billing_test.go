package accounts

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateCheckoutIntentReusesPendingSamePlan(t *testing.T) {
	clock := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "reuse-intent")
	first, err := store.CreateCheckoutIntent(context.Background(), account.ID, "pro", "terms", "refund", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateCheckoutIntent(context.Background(), account.ID, "pro", "terms", "refund", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Plan != "pro" || second.State != "pending" {
		t.Fatalf("reused intent = %#v, want pending pro %s", second, first.ID)
	}
	if _, err := store.CreateCheckoutIntent(context.Background(), account.ID, "scale", "terms", "refund", 30*time.Minute); !errors.Is(err, ErrConflict) {
		t.Fatalf("plan switch while pending = %v", err)
	}
}

func TestPaddleTransactionIsIdempotentAndSubscriptionIgnoresOlderEvents(t *testing.T) {
	clock := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "paddle")
	intent, err := store.CreateCheckoutIntent(context.Background(), account.ID, "pro", "terms-2026-08-16", "refund-2026-08-16", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	periodEnd := clock.Add(30 * 24 * time.Hour)
	newer := clock.Add(2 * time.Minute)
	_, applied, err := store.ApplySubscriptionUpdate(context.Background(), SubscriptionUpdate{
		SubscriptionID: "sub_aaaaaaaaaaaaaaaaaaaaaaaaaa", CustomerID: "ctm_aaaaaaaaaaaaaaaaaaaaaaaaaa", CheckoutIntentID: intent.ID,
		Plan: "pro", Status: "active", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: newer,
	})
	if err != nil || !applied {
		t.Fatalf("ApplySubscriptionUpdate() = %t, %v", applied, err)
	}
	_, applied, err = store.ApplySubscriptionUpdate(context.Background(), SubscriptionUpdate{
		SubscriptionID: "sub_aaaaaaaaaaaaaaaaaaaaaaaaaa", CustomerID: "ctm_aaaaaaaaaaaaaaaaaaaaaaaaaa", CheckoutIntentID: intent.ID,
		Plan: "pro", Status: "paused", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: clock,
	})
	if err != nil || applied {
		t.Fatalf("older update = %t, %v", applied, err)
	}
	subscription, err := store.CurrentSubscription(context.Background(), account.ID)
	if err != nil || subscription.Status != "active" || !subscription.LastEventAt.Equal(newer) {
		t.Fatalf("subscription = %#v, %v", subscription, err)
	}
	input := CompletedTransaction{
		TransactionID: "txn_aaaaaaaaaaaaaaaaaaaaaaaaaa", SubscriptionID: subscription.ID, CustomerID: "ctm_aaaaaaaaaaaaaaaaaaaaaaaaaa",
		CheckoutIntentID: intent.ID, Origin: "web", Plan: "pro", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: newer.Add(time.Minute),
	}
	accountID, remaining, created, err := store.ApplyCompletedTransaction(context.Background(), input)
	if err != nil || !created || accountID != account.ID || remaining != FreeMonthlyCredits+20_000 {
		t.Fatalf("ApplyCompletedTransaction() = %q, %d, %t, %v", accountID, remaining, created, err)
	}
	_, duplicateRemaining, created, err := store.ApplyCompletedTransaction(context.Background(), input)
	if err != nil || created || duplicateRemaining != remaining {
		t.Fatalf("duplicate transaction = %d, %t, %v", duplicateRemaining, created, err)
	}
	summary, err := store.CreditSummary(context.Background(), account.ID)
	if err != nil || summary.Granted != FreeMonthlyCredits+20_000 || len(summary.Grants) != 2 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
	completedIntent, err := store.GetCheckoutIntent(context.Background(), intent.ID)
	if err != nil || completedIntent.State != "completed" || completedIntent.PaddleTransactionID != input.TransactionID {
		t.Fatalf("intent = %#v, %v", completedIntent, err)
	}
}

func TestApprovedRefundRevokesUnusedCreditsAndChargebackSuspends(t *testing.T) {
	clock := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "refund")
	key, secret, err := store.CreateAPIKey(context.Background(), account.ID, "primary")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := store.CreateCheckoutIntent(context.Background(), account.ID, "scale", "terms", "refund", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// The paid grant must expire before the monthly free grant's month boundary
	// so this refund test spends (and then revokes) the paid grant, not the free.
	periodEnd := clock.Add(10 * 24 * time.Hour)
	input := CompletedTransaction{TransactionID: "txn_bbbbbbbbbbbbbbbbbbbbbbbbbb", SubscriptionID: "sub_bbbbbbbbbbbbbbbbbbbbbbbbbb", CustomerID: "ctm_bbbbbbbbbbbbbbbbbbbbbbbbbb", CheckoutIntentID: intent.ID, Origin: "web", Plan: "scale", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: clock}
	if _, _, _, err := store.ApplyCompletedTransaction(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	reservation, _, err := store.ReserveCredits(context.Background(), account.ID, key.ID, "paid-use", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitReservation(context.Background(), reservation.ID); err != nil {
		t.Fatal(err)
	}
	accountID, revoked, err := store.ApplyApprovedAdjustment(context.Background(), input.TransactionID, "adj_bbbbbbbbbbbbbbbbbbbbbbbbbb", "refund")
	if err != nil || accountID != account.ID || revoked != 99_999 {
		t.Fatalf("refund = %q, %d, %v", accountID, revoked, err)
	}
	summary, err := store.CreditSummary(context.Background(), account.ID)
	if err != nil || summary.Available != FreeMonthlyCredits || summary.Revoked != 99_999 || summary.Used != 1 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
	if _, _, err := store.ApplyApprovedAdjustment(context.Background(), input.TransactionID, "adj_bbbbbbbbbbbbbbbbbbbbbbbbbb", "refund"); err != nil {
		t.Fatalf("duplicate adjustment = %v", err)
	}
	if _, _, err := store.ApplyApprovedAdjustment(context.Background(), input.TransactionID, "adj_cccccccccccccccccccccccccc", "chargeback"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateAPIKey(context.Background(), secret); !errors.Is(err, ErrNotFound) {
		t.Fatalf("chargeback key authentication = %v", err)
	}
	updated, err := store.GetAccount(context.Background(), account.ID)
	if err != nil || updated.Status != AccountSuspended {
		t.Fatalf("account = %#v, %v", updated, err)
	}
}

func TestRefundAtomicallyCancelsPendingPaidReservationWithoutRestoringCredits(t *testing.T) {
	clock := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "refund-pending")
	key, _, err := store.CreateAPIKey(context.Background(), account.ID, "primary")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := store.CreateCheckoutIntent(context.Background(), account.ID, "scale", "terms", "refund", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// The paid grant must expire before the monthly free grant so the in-flight
	// reservation draws from the paid grant that the refund then cancels.
	periodEnd := clock.Add(10 * 24 * time.Hour)
	transaction := CompletedTransaction{
		TransactionID: "txn_pendingrefundpendingrefund", SubscriptionID: "sub_pendingrefundpendingrefund", CustomerID: "ctm_pendingrefundpendingrefund",
		CheckoutIntentID: intent.ID, Origin: "web", Plan: "scale", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: clock,
	}
	if _, _, _, err := store.ApplyCompletedTransaction(context.Background(), transaction); err != nil {
		t.Fatal(err)
	}
	reservation, _, err := store.ReserveCredits(context.Background(), account.ID, key.ID, "in-flight-paid-search", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, revoked, err := store.ApplyApprovedAdjustment(context.Background(), transaction.TransactionID, "adj_pendingrefundpendingrefund", "refund")
	if err != nil || revoked != 100_000 {
		t.Fatalf("refund = %d, %v", revoked, err)
	}
	if _, err := store.CommitReservation(context.Background(), reservation.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("commit after refund = %v", err)
	}
	remaining, err := store.ReleaseReservation(context.Background(), reservation.ID)
	if err != nil || remaining != FreeMonthlyCredits {
		t.Fatalf("idempotent release after refund = %d, %v", remaining, err)
	}
	summary, err := store.CreditSummary(context.Background(), account.ID)
	if err != nil || summary.Available != FreeMonthlyCredits || summary.Reserved != 0 || summary.Used != 0 || summary.Revoked != 100_000 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
}

func TestCanceledSubscriptionCanBeReplacedOnlyAfterPeriodEnd(t *testing.T) {
	clock := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "replacement")
	intent, err := store.CreateCheckoutIntent(context.Background(), account.ID, "pro", "terms", "refund", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	periodEnd := clock.Add(30 * 24 * time.Hour)
	oldID := "sub_oldoldoldoldoldoldoldoldold"
	_, _, err = store.ApplySubscriptionUpdate(context.Background(), SubscriptionUpdate{
		SubscriptionID: oldID, CustomerID: "ctm_replacementreplacementrepl", CheckoutIntentID: intent.ID,
		EventKey: "evt_scheduledcancel0000000000", Plan: "pro", Status: "active", PeriodStart: clock, PeriodEnd: periodEnd,
		CancelAtPeriodEnd: true, OccurredAt: clock.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.ApplyCompletedTransaction(context.Background(), CompletedTransaction{
		TransactionID: "txn_replacementinitialreplac", SubscriptionID: oldID, CustomerID: "ctm_replacementreplacementrepl",
		CheckoutIntentID: intent.ID, Origin: "web", Plan: "pro", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: clock.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateCheckoutIntent(context.Background(), account.ID, "scale", "terms", "refund", 30*time.Minute); !errors.Is(err, ErrConflict) {
		t.Fatalf("checkout before period end = %v", err)
	}
	clock = periodEnd
	if _, applied, err := store.ApplySubscriptionUpdate(context.Background(), SubscriptionUpdate{
		SubscriptionID: oldID, CustomerID: "ctm_replacementreplacementrepl", EventKey: "evt_effectivecancel0000000000",
		Plan: "pro", Status: "canceled", OccurredAt: clock,
	}); err != nil || !applied {
		t.Fatalf("effective cancellation = %t, %v", applied, err)
	}
	clock = periodEnd.Add(time.Minute)
	newIntent, err := store.CreateCheckoutIntent(context.Background(), account.ID, "scale", "terms", "refund", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	newID := "sub_newnewnewnewnewnewnewnewnew"
	_, applied, err := store.ApplySubscriptionUpdate(context.Background(), SubscriptionUpdate{
		SubscriptionID: newID, CustomerID: "ctm_replacementreplacementrepl", CheckoutIntentID: newIntent.ID,
		Plan: "scale", Status: "active", PeriodStart: clock, PeriodEnd: clock.Add(30 * 24 * time.Hour), OccurredAt: clock,
	})
	if err != nil || !applied {
		t.Fatalf("replacement subscription = %t, %v", applied, err)
	}
	current, err := store.CurrentSubscription(context.Background(), account.ID)
	if err != nil || current.ID != newID || current.Plan != "scale" {
		t.Fatalf("current subscription = %#v, %v", current, err)
	}
}

func TestPaddleRejectsMidPeriodPlanChangeAndUnboundPurchase(t *testing.T) {
	clock := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "no-proration")
	intent, err := store.CreateCheckoutIntent(context.Background(), account.ID, "pro", "terms", "refund", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	periodEnd := clock.Add(30 * 24 * time.Hour)
	subscription := SubscriptionUpdate{
		SubscriptionID: "sub_noprorationnoprorationno", CustomerID: "ctm_noprorationnoprorationno", CheckoutIntentID: intent.ID,
		Plan: "pro", Status: "active", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: clock,
	}
	if _, _, err := store.ApplySubscriptionUpdate(context.Background(), subscription); err != nil {
		t.Fatal(err)
	}
	changed := subscription
	changed.Plan = "scale"
	changed.OccurredAt = clock.Add(time.Minute)
	if _, _, err := store.ApplySubscriptionUpdate(context.Background(), changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("mid-period plan change = %v", err)
	}
	current, err := store.CurrentSubscription(context.Background(), account.ID)
	if err != nil || current.Plan != "pro" {
		t.Fatalf("current subscription = %#v, %v", current, err)
	}
	unbound := CompletedTransaction{
		TransactionID: "txn_unboundunboundunboundunbound", SubscriptionID: "sub_unknownunknownunknownunknown", CustomerID: subscription.CustomerID,
		Origin: "subscription_recurring", Plan: "pro", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: clock.Add(2 * time.Minute),
	}
	if _, _, _, err := store.ApplyCompletedTransaction(context.Background(), unbound); !errors.Is(err, ErrConflict) {
		t.Fatalf("unbound transaction = %v", err)
	}
}

func TestPaddleCheckoutIntentCannotExpireSwitchPlanOrGrantTwice(t *testing.T) {
	clock := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "intent-binding")
	intent, err := store.CreateCheckoutIntent(context.Background(), account.ID, "pro", "terms", "refund", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	periodEnd := clock.Add(30 * 24 * time.Hour)
	if _, _, err := store.ApplySubscriptionUpdate(context.Background(), SubscriptionUpdate{
		SubscriptionID: "sub_wrongplanwrongplanwrongplan", CustomerID: "ctm_intentbindingintentbinding", CheckoutIntentID: intent.ID,
		Plan: "scale", Status: "active", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: clock,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("intent plan switch = %v", err)
	}
	input := CompletedTransaction{
		TransactionID: "txn_boundboundboundboundbound", SubscriptionID: "sub_boundboundboundboundbound", CustomerID: "ctm_intentbindingintentbinding",
		CheckoutIntentID: intent.ID, Origin: "web", Plan: "pro", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: clock,
	}
	if _, _, created, err := store.ApplyCompletedTransaction(context.Background(), input); err != nil || !created {
		t.Fatalf("initial transaction = %t, %v", created, err)
	}
	second := input
	second.TransactionID = "txn_secondsecondsecondsecondse"
	if _, _, _, err := store.ApplyCompletedTransaction(context.Background(), second); !errors.Is(err, ErrConflict) {
		t.Fatalf("second transaction reused intent = %v", err)
	}
	summary, err := store.CreditSummary(context.Background(), account.ID)
	if err != nil || summary.Granted != FreeMonthlyCredits+20_000 {
		t.Fatalf("summary after reuse attempt = %#v, %v", summary, err)
	}

	expiredAccount := createTestAccount(t, store, "expired-intent")
	expiredIntent, err := store.CreateCheckoutIntent(context.Background(), expiredAccount.ID, "scale", "terms", "refund", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	expired := CompletedTransaction{
		TransactionID: "txn_expiredexpiredexpiredexpire", SubscriptionID: "sub_expiredexpiredexpiredexpire", CustomerID: "ctm_expiredexpiredexpiredexpire",
		CheckoutIntentID: expiredIntent.ID, Origin: "web", Plan: "scale", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: clock.Add(2 * time.Minute),
	}
	if _, _, _, err := store.ApplyCompletedTransaction(context.Background(), expired); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired intent transaction = %v", err)
	}
}

func TestPaddleEventDeduplicationRejectsPayloadCollision(t *testing.T) {
	clock := time.Date(2026, 8, 16, 3, 0, 0, 123456000, time.UTC)
	store := openTestStore(t, &clock)
	event := PaddleEvent{EventID: "evt_aaaaaaaaaaaaaaaaaaaaaaaaaa", EventType: "transaction.completed", OccurredAt: clock, Payload: []byte(`{"a":1}`)}
	stored, created, err := store.StorePaddleEvent(context.Background(), event)
	if err != nil || !created || stored.State != "pending" {
		t.Fatalf("StorePaddleEvent() = %#v, %t, %v", stored, created, err)
	}
	stored, created, err = store.StorePaddleEvent(context.Background(), event)
	if err != nil || created || stored.State != "pending" {
		t.Fatalf("duplicate = %#v, %t, %v", stored, created, err)
	}
	event.Payload = []byte(`{"a":2}`)
	if _, _, err := store.StorePaddleEvent(context.Background(), event); !errors.Is(err, ErrConflict) {
		t.Fatalf("collision error = %v", err)
	}
}

func TestPaddleEventsOrderFractionalTimestampsBeforeEventID(t *testing.T) {
	clock := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	later := PaddleEvent{EventID: "evt_aaaaaaaaaaaaaaaaaaaaaaaaaa", EventType: "subscription.paused", OccurredAt: clock.Add(900 * time.Microsecond), Payload: []byte(`{"later":true}`)}
	earlier := PaddleEvent{EventID: "evt_zzzzzzzzzzzzzzzzzzzzzzzzzz", EventType: "subscription.updated", OccurredAt: clock.Add(100 * time.Microsecond), Payload: []byte(`{"earlier":true}`)}
	if _, _, err := store.StorePaddleEvent(context.Background(), later); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.StorePaddleEvent(context.Background(), earlier); err != nil {
		t.Fatal(err)
	}
	pending, err := store.PendingPaddleEvents(context.Background(), 10)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending = %#v, %v", pending, err)
	}
	if pending[0].EventID != earlier.EventID || !pending[0].OccurredAt.Equal(earlier.OccurredAt) || pending[1].EventID != later.EventID || !pending[1].OccurredAt.Equal(later.OccurredAt) {
		t.Fatalf("fractional order = %#v", pending)
	}
}

func TestPaddleRecurringOriginIgnoresPropagatedCheckoutIntent(t *testing.T) {
	clock := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "recurring-origin")
	intent, err := store.CreateCheckoutIntent(context.Background(), account.ID, "pro", "terms", "refund", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionID := "sub_recurringoriginrecurringor"
	customerID := "ctm_recurringoriginrecurringor"
	periodEnd := clock.Add(30 * 24 * time.Hour)
	if _, _, err := store.ApplySubscriptionUpdate(context.Background(), SubscriptionUpdate{
		SubscriptionID: subscriptionID, CustomerID: customerID, CheckoutIntentID: intent.ID, EventKey: "evt_subscriptioncreated00000000",
		Plan: "pro", Status: "active", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: clock.Add(100 * time.Microsecond),
	}); err != nil {
		t.Fatal(err)
	}
	initial := CompletedTransaction{
		TransactionID: "txn_initialorigininitialorig", SubscriptionID: subscriptionID, CustomerID: customerID,
		CheckoutIntentID: intent.ID, Origin: "web", Plan: "pro", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: clock.Add(200 * time.Microsecond),
	}
	if _, _, created, err := store.ApplyCompletedTransaction(context.Background(), initial); err != nil || !created {
		t.Fatalf("initial transaction = %t, %v", created, err)
	}
	renewal := CompletedTransaction{
		TransactionID: "txn_recurringoriginrecurring", SubscriptionID: subscriptionID, CustomerID: customerID,
		CheckoutIntentID: intent.ID, Origin: "subscription_recurring", Plan: "pro", PeriodStart: periodEnd, PeriodEnd: periodEnd.Add(30 * 24 * time.Hour), OccurredAt: periodEnd.Add(200 * time.Microsecond),
	}
	if _, _, created, err := store.ApplyCompletedTransaction(context.Background(), renewal); err != nil || !created {
		t.Fatalf("renewal transaction = %t, %v", created, err)
	}
	completedIntent, err := store.GetCheckoutIntent(context.Background(), intent.ID)
	if err != nil || completedIntent.PaddleTransactionID != initial.TransactionID {
		t.Fatalf("checkout intent was rebound by renewal = %#v, %v", completedIntent, err)
	}
	summary, err := store.CreditSummary(context.Background(), account.ID)
	if err != nil || summary.Granted != FreeMonthlyCredits+40_000 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
	renewal.Origin = "subscription_update"
	renewal.TransactionID = "txn_subscriptionupdateignored"
	if _, _, _, err := store.ApplyCompletedTransaction(context.Background(), renewal); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsupported origin = %v", err)
	}
}

func TestSubscriptionLifecyclePreservesNullPeriodAndUsesDeterministicOrder(t *testing.T) {
	clock := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	store := openTestStore(t, &clock)
	account := createTestAccount(t, store, "subscription-order")
	intent, err := store.CreateCheckoutIntent(context.Background(), account.ID, "pro", "terms", "refund", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	periodEnd := clock.Add(30 * 24 * time.Hour)
	base := SubscriptionUpdate{
		SubscriptionID: "sub_fractionalorderfractional", CustomerID: "ctm_fractionalorderfractional", CheckoutIntentID: intent.ID,
		EventKey: "evt_10000000000000000000000000", Plan: "pro", Status: "active", PeriodStart: clock, PeriodEnd: periodEnd, OccurredAt: clock.Add(100 * time.Microsecond),
	}
	if _, applied, err := store.ApplySubscriptionUpdate(context.Background(), base); err != nil || !applied {
		t.Fatalf("active = %t, %v", applied, err)
	}
	pastDue := base
	pastDue.Status = "past_due"
	pastDue.EventKey = "evt_20000000000000000000000000"
	pastDue.OccurredAt = clock.Add(200 * time.Microsecond)
	if _, applied, err := store.ApplySubscriptionUpdate(context.Background(), pastDue); err != nil || !applied {
		t.Fatalf("past_due = %t, %v", applied, err)
	}
	paused := base
	paused.Status = "paused"
	paused.PeriodStart, paused.PeriodEnd = time.Time{}, time.Time{}
	paused.EventKey = "evt_50000000000000000000000000"
	paused.OccurredAt = clock.Add(900 * time.Microsecond)
	if _, applied, err := store.ApplySubscriptionUpdate(context.Background(), paused); err != nil || !applied {
		t.Fatalf("paused = %t, %v", applied, err)
	}
	current, err := store.CurrentSubscription(context.Background(), account.ID)
	if err != nil || current.Status != "paused" || !current.PeriodStart.Equal(clock) || !current.PeriodEnd.Equal(periodEnd) || !current.LastEventAt.Equal(paused.OccurredAt) {
		t.Fatalf("paused subscription = %#v, %v", current, err)
	}
	olderResume := base
	olderResume.EventKey = "evt_90000000000000000000000000"
	olderResume.OccurredAt = clock.Add(500 * time.Microsecond)
	if _, applied, err := store.ApplySubscriptionUpdate(context.Background(), olderResume); err != nil || applied {
		t.Fatalf("out-of-order resume = %t, %v", applied, err)
	}
	lowerTie := base
	lowerTie.EventKey = "evt_40000000000000000000000000"
	lowerTie.OccurredAt = paused.OccurredAt
	if _, applied, err := store.ApplySubscriptionUpdate(context.Background(), lowerTie); err != nil || applied {
		t.Fatalf("lower event-key tie = %t, %v", applied, err)
	}
	resume := base
	resume.EventKey = "evt_60000000000000000000000000"
	resume.OccurredAt = paused.OccurredAt
	if _, applied, err := store.ApplySubscriptionUpdate(context.Background(), resume); err != nil || !applied {
		t.Fatalf("higher event-key tie = %t, %v", applied, err)
	}
	canceled := paused
	canceled.Status = "canceled"
	canceled.EventKey = "evt_70000000000000000000000000"
	canceled.OccurredAt = clock.Add(time.Millisecond)
	if _, applied, err := store.ApplySubscriptionUpdate(context.Background(), canceled); err != nil || !applied {
		t.Fatalf("canceled = %t, %v", applied, err)
	}
	if _, err := store.CurrentSubscription(context.Background(), account.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("effective cancellation stayed current: %v", err)
	}
	var status string
	var start, end, currentFlag, lastNS int64
	var lastKey string
	if err := store.db.QueryRow(`SELECT status,current_period_start,current_period_end,is_current,last_event_at_ns,last_event_key FROM paddle_subscriptions WHERE paddle_subscription_id=?`, canceled.SubscriptionID).Scan(&status, &start, &end, &currentFlag, &lastNS, &lastKey); err != nil {
		t.Fatal(err)
	}
	if status != "canceled" || start != clock.Unix() || end != periodEnd.Unix() || currentFlag != 0 || lastNS != canceled.OccurredAt.UnixNano() || lastKey != canceled.EventKey {
		t.Fatalf("canceled row = %s %d %d current=%d order=%d/%q", status, start, end, currentFlag, lastNS, lastKey)
	}
}
