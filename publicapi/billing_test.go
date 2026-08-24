package publicapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eason4kim-rocket/annolux/accounts"
)

const testPaddleSecret = "pdl_ntfset_test_secret"

func attachTestBilling(t *testing.T, fixture *serverFixture, now time.Time) *BillingService {
	t.Helper()
	billing, err := NewBillingService(BillingConfig{
		Environment: "sandbox", APIKey: "pdl_sdbx_apikey", ClientToken: "test_client_token",
		WebhookSecret: testPaddleSecret, ProPriceID: "pri_pro", ScalePriceID: "pri_scale",
		TermsVersion: "terms-2026-08-16", RefundVersion: "refund-2026-08-16",
		Now: func() time.Time { return now },
	}, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.server.billing = billing
	return billing
}

func signPaddleBody(timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(testPaddleSecret))
	_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10) + ":"))
	_, _ = mac.Write(body)
	return "ts=" + strconv.FormatInt(timestamp, 10) + ";h1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestPaddleSignatureRequiresRawBodyAndFreshTimestamp(t *testing.T) {
	fixture := newServerFixture(t)
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	billing := attachTestBilling(t, &fixture, now)
	body := []byte(`{"event_id":"evt_aaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	header := signPaddleBody(now.Unix(), body)
	if err := billing.verifySignature(header, body); err != nil {
		t.Fatal(err)
	}
	if err := billing.verifySignature(header, append(body, ' ')); err == nil {
		t.Fatal("mutated raw body was accepted")
	}
	if err := billing.verifySignature(signPaddleBody(now.Add(-time.Minute).Unix(), body), body); err == nil {
		t.Fatal("stale signature was accepted")
	}
	badHeader := "ts=" + strconv.FormatInt(now.Unix(), 10) + ";h1=" + strings.Repeat("00", sha256.Size) + ";h1=" + strings.TrimPrefix(header[strings.Index(header, "h1="):], "h1=")
	if err := billing.verifySignature(badHeader, body); err != nil {
		t.Fatalf("one valid rotating signature should pass: %v", err)
	}
}

func TestCheckoutAndCompletedWebhookGrantCreditsExactlyOnce(t *testing.T) {
	fixture := newServerFixture(t)
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	attachTestBilling(t, &fixture, now)
	checkoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/billing/checkout-intents", strings.NewReader(`{"plan":"pro","accept_terms":true,"accept_refund":true}`))
	checkoutRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	checkoutRequest.Header.Set("X-CSRF-Token", fixture.csrf)
	checkoutRequest.AddCookie(&http.Cookie{Name: SessionCookieName, Value: fixture.sessionToken})
	checkoutRecorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(checkoutRecorder, checkoutRequest)
	if checkoutRecorder.Code != http.StatusCreated {
		t.Fatalf("checkout = %d %s", checkoutRecorder.Code, checkoutRecorder.Body.String())
	}
	var checkout struct {
		Intent string `json:"checkout_intent"`
		Price  string `json:"price_id"`
	}
	if err := json.Unmarshal(checkoutRecorder.Body.Bytes(), &checkout); err != nil || checkout.Intent == "" || checkout.Price != "pri_pro" {
		t.Fatalf("checkout body = %#v, %v", checkout, err)
	}
	periodEnd := now.Add(30 * 24 * time.Hour)
	event := map[string]any{
		"event_id": "evt_aaaaaaaaaaaaaaaaaaaaaaaaaa", "event_type": "transaction.completed", "occurred_at": now.Add(time.Minute),
		"notification_id": "ntf_aaaaaaaaaaaaaaaaaaaaaaaaaa",
		"data": map[string]any{
			"id": "txn_aaaaaaaaaaaaaaaaaaaaaaaaaa", "origin": "web", "status": "completed",
			"customer_id": "ctm_aaaaaaaaaaaaaaaaaaaaaaaaaa", "subscription_id": "sub_aaaaaaaaaaaaaaaaaaaaaaaaaa",
			"billing_period": map[string]any{"starts_at": now, "ends_at": periodEnd},
			"custom_data":    map[string]string{"checkout_intent_id": checkout.Intent},
			"items":          []any{map[string]any{"price": map[string]string{"id": "pri_pro"}}},
		},
	}
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	postWebhook := func(body []byte) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/paddle", bytes.NewReader(body))
		request.Header.Set("Paddle-Signature", signPaddleBody(now.Unix(), body))
		recorder := httptest.NewRecorder()
		fixture.server.Handler().ServeHTTP(recorder, request)
		return recorder
	}
	if response := postWebhook(body); response.Code != http.StatusOK {
		t.Fatalf("webhook = %d %s", response.Code, response.Body.String())
	}
	if response := postWebhook(body); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"duplicate":true`) {
		t.Fatalf("duplicate = %d %s", response.Code, response.Body.String())
	}
	summary, err := fixture.store.CreditSummary(context.Background(), fixture.account.ID)
	if err != nil || summary.Available != accounts.FreeMonthlyCredits+20_000 || summary.Granted != accounts.FreeMonthlyCredits+20_000 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
	mutated := bytes.Replace(body, []byte(`"status":"completed"`), []byte(`"status":"paid"`), 1)
	if response := postWebhook(mutated); response.Code != http.StatusConflict {
		t.Fatalf("event collision = %d %s", response.Code, response.Body.String())
	}
}

func TestRenewalWebhookUsesOriginAndDoesNotRebindPropagatedIntent(t *testing.T) {
	fixture := newServerFixture(t)
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	attachTestBilling(t, &fixture, now)
	intent, err := fixture.store.CreateCheckoutIntent(context.Background(), fixture.account.ID, "pro", "terms-2026-08-16", "refund-2026-08-16", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	periodEnd := now.Add(30 * 24 * time.Hour)
	subscriptionID := "sub_webhookrenewalwebhookrenew"
	customerID := "ctm_webhookrenewalwebhookrenew"
	post := func(event map[string]any) *httptest.ResponseRecorder {
		body, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/paddle", bytes.NewReader(body))
		request.Header.Set("Paddle-Signature", signPaddleBody(now.Unix(), body))
		recorder := httptest.NewRecorder()
		fixture.server.Handler().ServeHTTP(recorder, request)
		return recorder
	}
	subscription := map[string]any{
		"event_id": "evt_subscriptioncreated00000000", "event_type": "subscription.created", "occurred_at": now.Add(100 * time.Microsecond),
		"notification_id": "ntf_subscriptioncreated00000000",
		"data": map[string]any{
			"id": subscriptionID, "status": "active", "customer_id": customerID,
			"current_billing_period": map[string]any{"starts_at": now, "ends_at": periodEnd},
			"custom_data":            map[string]string{"checkout_intent_id": intent.ID},
			"items":                  []any{map[string]any{"price": map[string]string{"id": "pri_pro"}}},
		},
	}
	if response := post(subscription); response.Code != http.StatusOK {
		t.Fatalf("subscription.created = %d %s", response.Code, response.Body.String())
	}
	initialID := "txn_webhookinitialwebhookinit"
	initial := map[string]any{
		"event_id": "evt_initialtransaction0000000", "event_type": "transaction.completed", "occurred_at": now.Add(200 * time.Microsecond),
		"notification_id": "ntf_initialtransaction0000000",
		"data": map[string]any{
			"id": initialID, "origin": "web", "status": "completed", "customer_id": customerID, "subscription_id": subscriptionID,
			"billing_period": map[string]any{"starts_at": now, "ends_at": periodEnd},
			"custom_data":    map[string]string{"checkout_intent_id": intent.ID},
			"items":          []any{map[string]any{"price": map[string]string{"id": "pri_pro"}}},
		},
	}
	if response := post(initial); response.Code != http.StatusOK {
		t.Fatalf("initial transaction = %d %s", response.Code, response.Body.String())
	}
	renewal := map[string]any{
		"event_id": "evt_recurringtransaction000000", "event_type": "transaction.completed", "occurred_at": periodEnd.Add(300 * time.Microsecond),
		"notification_id": "ntf_recurringtransaction000000",
		"data": map[string]any{
			"id": "txn_webhookrecurringwebhookre", "origin": "subscription_recurring", "status": "completed", "customer_id": customerID, "subscription_id": subscriptionID,
			"billing_period": map[string]any{"starts_at": periodEnd, "ends_at": periodEnd.Add(30 * 24 * time.Hour)},
			// Paddle officially copies checkout custom_data to the subscription,
			// then from the subscription to every recurring transaction.
			"custom_data": map[string]string{"checkout_intent_id": intent.ID},
			"items":       []any{map[string]any{"price": map[string]string{"id": "pri_pro"}}},
		},
	}
	if response := post(renewal); response.Code != http.StatusOK {
		t.Fatalf("recurring transaction = %d %s", response.Code, response.Body.String())
	}
	completedIntent, err := fixture.store.GetCheckoutIntent(context.Background(), intent.ID)
	if err != nil || completedIntent.PaddleTransactionID != initialID {
		t.Fatalf("checkout intent was rebound = %#v, %v", completedIntent, err)
	}
	summary, err := fixture.store.CreditSummary(context.Background(), fixture.account.ID)
	if err != nil || summary.Granted != accounts.FreeMonthlyCredits+40_000 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
}

func TestUnrelatedAndNonRecurringTransactionsAreStoredIgnored(t *testing.T) {
	fixture := newServerFixture(t)
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	attachTestBilling(t, &fixture, now)
	post := func(event map[string]any) {
		body, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/paddle", bytes.NewReader(body))
		request.Header.Set("Paddle-Signature", signPaddleBody(now.Unix(), body))
		recorder := httptest.NewRecorder()
		fixture.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("ignored webhook = %d %s", recorder.Code, recorder.Body.String())
		}
		eventID, _ := event["event_id"].(string)
		stored, err := fixture.store.GetPaddleEvent(context.Background(), eventID)
		if err != nil || stored.State != "ignored" {
			t.Fatalf("stored event = %#v, %v", stored, err)
		}
	}
	post(map[string]any{
		"event_id": "evt_unrelatedtransaction000000", "event_type": "transaction.completed", "occurred_at": now.Add(time.Microsecond),
		"data": map[string]any{
			"id": "txn_unrelatedtransaction00000", "origin": "web", "status": "completed",
			"customer_id": "ctm_unrelatedtransaction00000", "subscription_id": nil, "billing_period": nil, "custom_data": nil,
			"items": []any{map[string]any{"price": map[string]string{"id": "pri_other_vendor_product"}}},
		},
	})
	post(map[string]any{
		"event_id": "evt_subscriptionupdatecharge00", "event_type": "transaction.completed", "occurred_at": now.Add(2 * time.Microsecond),
		"data": map[string]any{
			"id": "txn_subscriptionupdatecharge0", "origin": "subscription_update", "status": "completed",
			"customer_id": "ctm_subscriptionupdatecharge0", "subscription_id": "sub_subscriptionupdatecharge0", "billing_period": nil, "custom_data": nil,
			"items": []any{map[string]any{"price": map[string]string{"id": "pri_pro"}}},
		},
	})
	summary, err := fixture.store.CreditSummary(context.Background(), fixture.account.ID)
	if err != nil || summary.Granted != accounts.FreeMonthlyCredits {
		t.Fatalf("ignored transactions changed credits = %#v, %v", summary, err)
	}
}

func TestCheckoutIntentRejectsTrailingJSON(t *testing.T) {
	fixture := newServerFixture(t)
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	attachTestBilling(t, &fixture, now)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/billing/checkout-intents", strings.NewReader(`{"plan":"pro","accept_terms":true,"accept_refund":true}{"plan":"scale"}`))
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	request.Header.Set("X-CSRF-Token", fixture.csrf)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: fixture.sessionToken})
	recorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestPaddleLiveStaysFrozenWithoutExplicitGate(t *testing.T) {
	fixture := newServerFixture(t)
	_, err := NewBillingService(BillingConfig{
		Environment: "live", APIKey: "pdl_live", ClientToken: "live_token", WebhookSecret: "secret",
		ProPriceID: "pri_pro", ScalePriceID: "pri_scale", TermsVersion: "terms", RefundVersion: "refund",
	}, fixture.store)
	if err == nil || !strings.Contains(err.Error(), "live is frozen") {
		t.Fatalf("live configuration error = %v", err)
	}
}

func TestPaddleDefaultClientDoesNotFollowRedirects(t *testing.T) {
	fixture := newServerFixture(t)
	billing, err := NewBillingService(BillingConfig{
		Environment: "sandbox", APIKey: "pdl_sdbx_apikey", ClientToken: "test_client_token",
		WebhookSecret: testPaddleSecret, ProPriceID: "pri_pro", ScalePriceID: "pri_scale",
		TermsVersion: "terms-2026-08-16", RefundVersion: "refund-2026-08-16",
	}, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	if billing.client.CheckRedirect == nil {
		t.Fatal("default Paddle client would follow redirects carrying its API key")
	}
	redirectRequest := httptest.NewRequest(http.MethodGet, "https://sandbox-api.paddle.com/transactions", nil)
	if err := billing.client.CheckRedirect(redirectRequest, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy = %v", err)
	}
}

func TestPaddleReconciliationRepairsMissedEventsExactlyOnce(t *testing.T) {
	fixture := newServerFixture(t)
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	intent, err := fixture.store.CreateCheckoutIntent(context.Background(), fixture.account.ID, "pro", "terms-2026-08-16", "refund-2026-08-16", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	periodEnd := now.Add(30 * 24 * time.Hour)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer pdl_sdbx_apikey" || request.Header.Get("Skip-Count") != "true" {
			t.Fatalf("Paddle reconciliation headers = %v", request.Header)
		}
		var body string
		switch request.URL.Path {
		case "/transactions":
			if request.URL.Query().Get("status") != "completed" || request.URL.Query().Get("updated_at[GTE]") == "" {
				t.Fatalf("transaction query = %s", request.URL.RawQuery)
			}
			body = fmt.Sprintf(`{"data":[{"id":"txn_reconcile","origin":"web","status":"completed","customer_id":"ctm_reconcile","subscription_id":"sub_reconcile","billing_period":{"starts_at":%q,"ends_at":%q},"custom_data":{"checkout_intent_id":%q},"items":[{"price":{"id":"pri_pro"}}],"updated_at":%q}],"meta":{"pagination":{"has_more":false,"next":null}}}`, now.Format(time.RFC3339), periodEnd.Format(time.RFC3339), intent.ID, now.Add(time.Minute).Format(time.RFC3339))
		case "/subscriptions":
			body = fmt.Sprintf(`{"data":[{"id":"sub_reconcile","status":"active","customer_id":"ctm_reconcile","current_billing_period":{"starts_at":%q,"ends_at":%q},"custom_data":{"checkout_intent_id":%q},"items":[{"price":{"id":"pri_pro"}}],"updated_at":%q}],"meta":{"pagination":{"has_more":false,"next":null}}}`, now.Format(time.RFC3339), periodEnd.Format(time.RFC3339), intent.ID, now.Add(2*time.Minute).Format(time.RFC3339))
		case "/adjustments":
			body = `{"data":[],"meta":{"pagination":{"has_more":false,"next":null}}}`
		default:
			t.Fatalf("unexpected Paddle reconciliation request: %s", request.URL)
		}
		return oauthJSONResponse(http.StatusOK, body), nil
	})}
	billing, err := NewBillingService(BillingConfig{
		Environment: "sandbox", APIKey: "pdl_sdbx_apikey", ClientToken: "test_client_token",
		WebhookSecret: testPaddleSecret, ProPriceID: "pri_pro", ScalePriceID: "pri_scale",
		TermsVersion: "terms-2026-08-16", RefundVersion: "refund-2026-08-16",
		HTTPClient: client, Now: func() time.Time { return now },
	}, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		stats, reconcileErr := billing.ReconcileSince(context.Background(), now.Add(-time.Hour))
		if reconcileErr != nil || stats.Transactions != 1 || stats.Subscriptions != 1 {
			t.Fatalf("reconcile attempt %d = %#v, %v", attempt, stats, reconcileErr)
		}
	}
	summary, err := fixture.store.CreditSummary(context.Background(), fixture.account.ID)
	if err != nil || summary.Available != accounts.FreeMonthlyCredits+20_000 || summary.Granted != accounts.FreeMonthlyCredits+20_000 {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
}

func TestPaddleReconciliationAppliesNullPeriodPauseAndCancellation(t *testing.T) {
	fixture := newServerFixture(t)
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	intent, err := fixture.store.CreateCheckoutIntent(context.Background(), fixture.account.ID, "pro", "terms-2026-08-16", "refund-2026-08-16", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	periodEnd := now.Add(30 * 24 * time.Hour)
	subscriptionID := "sub_reconcilenullperiodreconc"
	customerID := "ctm_reconcilenullperiodreconc"
	if _, _, err := fixture.store.ApplySubscriptionUpdate(context.Background(), accounts.SubscriptionUpdate{
		SubscriptionID: subscriptionID, CustomerID: customerID, CheckoutIntentID: intent.ID, EventKey: "evt_reconcileinitial000000000",
		Plan: "pro", Status: "active", PeriodStart: now, PeriodEnd: periodEnd, OccurredAt: now.Add(100 * time.Microsecond),
	}); err != nil {
		t.Fatal(err)
	}
	status := "paused"
	updatedAt := now.Add(time.Minute + 987654*time.Microsecond)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body string
		switch request.URL.Path {
		case "/transactions", "/adjustments":
			body = `{"data":[],"meta":{"pagination":{"has_more":false,"next":null}}}`
		case "/subscriptions":
			body = fmt.Sprintf(`{"data":[{"id":%q,"status":%q,"customer_id":%q,"current_billing_period":null,"custom_data":{"checkout_intent_id":%q},"items":[{"price":{"id":"pri_pro"}}],"updated_at":%q}],"meta":{"pagination":{"has_more":false,"next":null}}}`, subscriptionID, status, customerID, intent.ID, updatedAt.Format(time.RFC3339Nano))
		default:
			t.Fatalf("unexpected Paddle reconciliation request: %s", request.URL)
		}
		return oauthJSONResponse(http.StatusOK, body), nil
	})}
	billing, err := NewBillingService(BillingConfig{
		Environment: "sandbox", APIKey: "pdl_sdbx_apikey", ClientToken: "test_client_token", WebhookSecret: testPaddleSecret,
		ProPriceID: "pri_pro", ScalePriceID: "pri_scale", TermsVersion: "terms-2026-08-16", RefundVersion: "refund-2026-08-16",
		HTTPClient: client, Now: func() time.Time { return now.Add(5 * time.Minute) },
	}, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := billing.ReconcileSince(context.Background(), now.Add(-time.Hour))
	if err != nil || stats.Subscriptions != 1 {
		t.Fatalf("pause reconciliation = %#v, %v", stats, err)
	}
	current, err := fixture.store.CurrentSubscription(context.Background(), fixture.account.ID)
	if err != nil || current.Status != "paused" || !current.PeriodStart.Equal(now) || !current.PeriodEnd.Equal(periodEnd) || !current.LastEventAt.Equal(updatedAt) {
		t.Fatalf("paused subscription = %#v, %v", current, err)
	}
	status = "canceled"
	updatedAt = now.Add(2*time.Minute + 123456*time.Microsecond)
	stats, err = billing.ReconcileSince(context.Background(), now.Add(-time.Hour))
	if err != nil || stats.Subscriptions != 1 {
		t.Fatalf("cancel reconciliation = %#v, %v", stats, err)
	}
	if _, err := fixture.store.CurrentSubscription(context.Background(), fixture.account.ID); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatalf("canceled subscription stayed current: %v", err)
	}
	portalCustomer, portalSubscription, err := fixture.store.PaddlePortalTarget(context.Background(), fixture.account.ID)
	if err != nil || portalCustomer != customerID || portalSubscription != "" {
		t.Fatalf("portal target = %q/%q, %v", portalCustomer, portalSubscription, err)
	}
}

func TestPaddleReconciliationRejectsCrossOriginPagination(t *testing.T) {
	fixture := newServerFixture(t)
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return oauthJSONResponse(http.StatusOK, `{"data":[],"meta":{"pagination":{"has_more":true,"next":"https://attacker.example/transactions"}}}`), nil
	})}
	billing, err := NewBillingService(BillingConfig{
		Environment: "sandbox", APIKey: "pdl_sdbx_apikey", ClientToken: "test_client_token",
		WebhookSecret: testPaddleSecret, ProPriceID: "pri_pro", ScalePriceID: "pri_scale",
		TermsVersion: "terms-2026-08-16", RefundVersion: "refund-2026-08-16",
		HTTPClient: client, Now: func() time.Time { return now },
	}, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := billing.ReconcileSince(context.Background(), now.Add(-time.Hour)); err == nil || !strings.Contains(err.Error(), "unsafe URL") {
		t.Fatalf("cross-origin pagination error = %v", err)
	}
}
