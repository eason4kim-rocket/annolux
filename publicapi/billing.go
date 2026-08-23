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
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eason4kim-rocket/annolux/accounts"
)

const maximumPaddleWebhookBody = 1 << 20

type BillingConfig struct {
	Environment        string
	APIKey             string
	ClientToken        string
	WebhookSecret      string
	ProPriceID         string
	ScalePriceID       string
	TermsVersion       string
	RefundVersion      string
	LiveEnabled        bool
	HTTPClient         *http.Client
	Now                func() time.Time
	SignatureTolerance time.Duration
}

func (config BillingConfig) Enabled() bool {
	return config.APIKey != "" || config.ClientToken != "" || config.WebhookSecret != "" || config.ProPriceID != "" || config.ScalePriceID != ""
}

type BillingService struct {
	accounts      *accounts.Store
	environment   string
	apiBase       string
	apiKey        string
	clientToken   string
	webhookSecret string
	pricePlans    map[string]string
	planPrices    map[string]string
	termsVersion  string
	refundVersion string
	client        *http.Client
	now           func() time.Time
	tolerance     time.Duration
}

// ReprocessPending retries previously persisted events in chronological order.
// Side effects remain idempotent because event IDs, Paddle entity IDs, grants,
// and ledger references are all independently unique.
func (service *BillingService) ReprocessPending(ctx context.Context, limit int) (int, error) {
	if service == nil || ctx == nil {
		return 0, errors.New("billing service is unavailable")
	}
	events, err := service.accounts.PendingPaddleEvents(ctx, limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, stored := range events {
		event, err := decodePaddleEvent(stored.Payload)
		if err == nil {
			err = service.processEvent(ctx, event)
		}
		ignored := errors.Is(err, errPaddleEventIgnored)
		if err != nil && !ignored {
			_ = service.accounts.SetPaddleEventState(ctx, stored.EventID, "failed", sanitizeEventError(err))
			return processed, err
		}
		state := "processed"
		if ignored || !supportedPaddleEvent(event.EventType) {
			state = "ignored"
		}
		if err := service.accounts.SetPaddleEventState(ctx, stored.EventID, state, ""); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func NewBillingService(config BillingConfig, store *accounts.Store) (*BillingService, error) {
	if store == nil || config.APIKey == "" || config.ClientToken == "" || config.WebhookSecret == "" || config.ProPriceID == "" || config.ScalePriceID == "" || config.TermsVersion == "" || config.RefundVersion == "" {
		return nil, errors.New("publicapi: incomplete Paddle configuration")
	}
	if config.Environment == "" {
		config.Environment = "sandbox"
	}
	apiBase := "https://sandbox-api.paddle.com"
	if config.Environment == "live" {
		if !config.LiveEnabled {
			return nil, errors.New("publicapi: Paddle live is frozen pending seller support requirements")
		}
		apiBase = "https://api.paddle.com"
	} else if config.Environment != "sandbox" {
		return nil, errors.New("publicapi: invalid Paddle environment")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.SignatureTolerance == 0 {
		config.SignatureTolerance = 5 * time.Second
	}
	if config.SignatureTolerance < time.Second || config.SignatureTolerance > 5*time.Minute {
		return nil, errors.New("publicapi: invalid Paddle signature tolerance")
	}
	return &BillingService{
		accounts: store, environment: config.Environment, apiBase: apiBase, apiKey: config.APIKey,
		clientToken: config.ClientToken, webhookSecret: config.WebhookSecret,
		pricePlans:   map[string]string{config.ProPriceID: "pro", config.ScalePriceID: "scale"},
		planPrices:   map[string]string{"pro": config.ProPriceID, "scale": config.ScalePriceID},
		termsVersion: config.TermsVersion, refundVersion: config.RefundVersion,
		client: config.HTTPClient, now: config.Now, tolerance: config.SignatureTolerance,
	}, nil
}

func (server *Server) handleCheckoutIntent(writer http.ResponseWriter, request *http.Request) {
	if server.billing == nil {
		writeError(writer, http.StatusServiceUnavailable, "BILLING_UNAVAILABLE", "paid checkout is not available")
		return
	}
	session, ok := server.requireWriteSession(writer, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 8<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Plan         string `json:"plan"`
		AcceptTerms  bool   `json:"accept_terms"`
		AcceptRefund bool   `json:"accept_refund"`
	}
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "POLICY_ACCEPTANCE_REQUIRED", "Terms and Refund Policy must be accepted")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || !input.AcceptTerms || !input.AcceptRefund {
		writeError(writer, http.StatusBadRequest, "POLICY_ACCEPTANCE_REQUIRED", "Terms and Refund Policy must be accepted")
		return
	}
	priceID, ok := server.billing.planPrices[input.Plan]
	if !ok {
		writeError(writer, http.StatusBadRequest, "INVALID_INPUT", "plan must be pro or scale")
		return
	}
	intent, err := server.accounts.CreateCheckoutIntent(request.Context(), session.Session.AccountID, input.Plan, server.billing.termsVersion, server.billing.refundVersion, 30*time.Minute)
	if errors.Is(err, accounts.ErrConflict) {
		writeError(writer, http.StatusConflict, "SUBSCRIPTION_CONFLICT", "an active subscription or checkout already exists")
		return
	}
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "BILLING_UNAVAILABLE", "checkout could not be started")
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{
		"checkout_intent": intent.ID,
		"environment":     server.billing.environment,
		"client_token":    server.billing.clientToken,
		"price_id":        priceID,
		"custom_data":     map[string]string{"checkout_intent_id": intent.ID},
		"expires_at":      intent.ExpiresAt,
	})
}

func (server *Server) handlePortalSession(writer http.ResponseWriter, request *http.Request) {
	if server.billing == nil {
		writeError(writer, http.StatusServiceUnavailable, "BILLING_UNAVAILABLE", "billing portal is not available")
		return
	}
	session, ok := server.requireWriteSession(writer, request)
	if !ok {
		return
	}
	customerID, subscriptionID, err := server.accounts.PaddlePortalTarget(request.Context(), session.Session.AccountID)
	if errors.Is(err, accounts.ErrNotFound) {
		writeError(writer, http.StatusNotFound, "NO_SUBSCRIPTION", "no billing account exists")
		return
	}
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "BILLING_UNAVAILABLE", "billing portal is unavailable")
		return
	}
	portalURL, err := server.billing.createPortalSession(request.Context(), customerID, subscriptionID)
	if err != nil {
		server.logger.Error("Paddle portal session failed", "error", err)
		writeError(writer, http.StatusBadGateway, "BILLING_FAILED", "billing portal could not be opened")
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]string{"url": portalURL})
}

func (service *BillingService) createPortalSession(ctx context.Context, customerID, subscriptionID string) (string, error) {
	payload := map[string]any{}
	if subscriptionID != "" {
		payload["subscription_ids"] = []string{subscriptionID}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	endpoint := service.apiBase + "/customers/" + url.PathEscape(customerID) + "/portal-sessions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+service.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := service.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return "", fmt.Errorf("Paddle portal status %d", response.StatusCode)
	}
	var envelope struct {
		Data struct {
			URLs struct {
				General struct {
					Overview string `json:"overview"`
				} `json:"general"`
			} `json:"urls"`
		} `json:"data"`
	}
	if err := decodeBoundedJSON(response.Body, 1<<20, &envelope); err != nil {
		return "", err
	}
	parsed, err := url.Parse(envelope.Data.URLs.General.Overview)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || (parsed.Port() != "" && parsed.Port() != "443") ||
		!(parsed.Hostname() == "paddle.com" || strings.HasSuffix(parsed.Hostname(), ".paddle.com")) {
		return "", errors.New("Paddle returned an invalid portal URL")
	}
	return parsed.String(), nil
}

func decodeBoundedJSON(reader io.Reader, maximum int64, destination any) error {
	if reader == nil || maximum < 1 || destination == nil {
		return errors.New("invalid bounded JSON input")
	}
	body, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > maximum {
		return errors.New("JSON response is too large")
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return err
	}
	return nil
}

func (server *Server) handlePaddleWebhook(writer http.ResponseWriter, request *http.Request) {
	if server.billing == nil {
		writeError(writer, http.StatusServiceUnavailable, "BILLING_UNAVAILABLE", "Paddle webhook is not configured")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maximumPaddleWebhookBody)
	rawBody, err := io.ReadAll(request.Body)
	if err != nil || len(rawBody) == 0 || len(rawBody) > maximumPaddleWebhookBody {
		writeError(writer, http.StatusBadRequest, "INVALID_WEBHOOK", "invalid webhook body")
		return
	}
	if err := server.billing.verifySignature(request.Header.Get("Paddle-Signature"), rawBody); err != nil {
		writeError(writer, http.StatusUnauthorized, "INVALID_WEBHOOK_SIGNATURE", "invalid webhook signature")
		return
	}
	event, err := decodePaddleEvent(rawBody)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "INVALID_WEBHOOK", "invalid webhook event")
		return
	}
	stored, _, err := server.accounts.StorePaddleEvent(request.Context(), accounts.PaddleEvent{
		EventID: event.EventID, EventType: event.EventType, OccurredAt: event.OccurredAt, Payload: append([]byte(nil), rawBody...),
	})
	if err != nil {
		writeError(writer, http.StatusConflict, "WEBHOOK_CONFLICT", "webhook event conflicts with stored data")
		return
	}
	if stored.State == "processed" || stored.State == "ignored" {
		writeJSON(writer, http.StatusOK, map[string]bool{"received": true, "duplicate": true})
		return
	}
	processErr := server.billing.processEvent(request.Context(), event)
	ignored := errors.Is(processErr, errPaddleEventIgnored)
	if processErr != nil && !ignored {
		_ = server.accounts.SetPaddleEventState(request.Context(), event.EventID, "failed", sanitizeEventError(processErr))
		server.logger.Error("Paddle event processing failed", "event_id", event.EventID, "event_type", event.EventType, "error", processErr)
		writeError(writer, http.StatusInternalServerError, "WEBHOOK_PROCESSING_FAILED", "webhook could not be processed")
		return
	}
	state := "processed"
	if ignored || !supportedPaddleEvent(event.EventType) {
		state = "ignored"
	}
	if err := server.accounts.SetPaddleEventState(request.Context(), event.EventID, state, ""); err != nil {
		writeError(writer, http.StatusInternalServerError, "WEBHOOK_PROCESSING_FAILED", "webhook could not be finalized")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"received": true})
}

func (service *BillingService) verifySignature(header string, rawBody []byte) error {
	var timestamp int64
	var signatures [][]byte
	seenTimestamp := false
	for _, part := range strings.Split(header, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || value == "" {
			continue
		}
		switch key {
		case "ts":
			if seenTimestamp {
				return errors.New("duplicate Paddle timestamp")
			}
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return err
			}
			timestamp, seenTimestamp = parsed, true
		case "h1":
			decoded, err := hex.DecodeString(value)
			if err == nil && len(decoded) == sha256.Size {
				signatures = append(signatures, decoded)
			}
		}
	}
	if !seenTimestamp || len(signatures) == 0 {
		return errors.New("incomplete Paddle signature")
	}
	delta := service.now().UTC().Sub(time.Unix(timestamp, 0).UTC())
	if delta < -service.tolerance || delta > service.tolerance {
		return errors.New("Paddle signature timestamp is outside tolerance")
	}
	mac := hmac.New(sha256.New, []byte(service.webhookSecret))
	_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	_, _ = mac.Write([]byte(":"))
	_, _ = mac.Write(rawBody)
	expected := mac.Sum(nil)
	for _, signature := range signatures {
		if hmac.Equal(expected, signature) {
			return nil
		}
	}
	return errors.New("Paddle signature mismatch")
}

type paddleEventEnvelope struct {
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Data       json.RawMessage `json:"data"`
}

func decodePaddleEvent(raw []byte) (paddleEventEnvelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var event paddleEventEnvelope
	if err := decoder.Decode(&event); err != nil || !strings.HasPrefix(event.EventID, "evt_") || event.EventType == "" || event.OccurredAt.IsZero() || len(event.Data) == 0 {
		return paddleEventEnvelope{}, errors.New("invalid Paddle event")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return paddleEventEnvelope{}, errors.New("trailing Paddle event data")
	}
	return event, nil
}

func supportedPaddleEvent(eventType string) bool {
	return eventType == "transaction.completed" || supportedSubscriptionEvent(eventType) || eventType == "adjustment.created" || eventType == "adjustment.updated"
}

func supportedSubscriptionEvent(eventType string) bool {
	switch eventType {
	case "subscription.created", "subscription.updated", "subscription.activated", "subscription.trialing", "subscription.past_due", "subscription.paused", "subscription.resumed", "subscription.canceled":
		return true
	default:
		return false
	}
}

func (service *BillingService) processEvent(ctx context.Context, event paddleEventEnvelope) error {
	switch {
	case event.EventType == "transaction.completed":
		return service.processCompletedTransaction(ctx, event)
	case supportedSubscriptionEvent(event.EventType):
		return service.processSubscription(ctx, event)
	case event.EventType == "adjustment.created" || event.EventType == "adjustment.updated":
		return service.processAdjustment(ctx, event)
	default:
		return nil
	}
}

var errPaddleEventIgnored = errors.New("Paddle event is outside the Annolux billing catalog")

type paddleBillingPeriod struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

type paddleItem struct {
	Price struct {
		ID string `json:"id"`
	} `json:"price"`
}

func (service *BillingService) planFromItems(items []paddleItem) (string, error) {
	plan := ""
	for _, item := range items {
		candidate := service.pricePlans[item.Price.ID]
		if candidate == "" {
			continue
		}
		if plan != "" && plan != candidate {
			return "", errors.New("transaction contains multiple Annolux plans")
		}
		plan = candidate
	}
	if plan == "" {
		return "", errNoAnnoluxPlan
	}
	return plan, nil
}

var errNoAnnoluxPlan = errors.New("transaction contains no Annolux plan")

func checkoutIntentID(customData map[string]any) string {
	value, _ := customData["checkout_intent_id"].(string)
	if strings.HasPrefix(value, "chk_") && len(value) <= 128 {
		return value
	}
	return ""
}

func (service *BillingService) processCompletedTransaction(ctx context.Context, event paddleEventEnvelope) error {
	var data struct {
		ID             string               `json:"id"`
		Origin         string               `json:"origin"`
		Status         string               `json:"status"`
		CustomerID     string               `json:"customer_id"`
		SubscriptionID string               `json:"subscription_id"`
		BillingPeriod  *paddleBillingPeriod `json:"billing_period"`
		CustomData     map[string]any       `json:"custom_data"`
		Items          []paddleItem         `json:"items"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil || data.Status != "completed" {
		return errors.New("completed transaction payload is incomplete")
	}
	if data.Origin != "web" && data.Origin != "subscription_recurring" {
		// Only checkout purchases and true billing-cycle renewals grant plan
		// credits. Paddle also emits completed transactions for upgrades,
		// one-time charges, and payment-method changes; those must not grant.
		return errPaddleEventIgnored
	}
	plan, err := service.planFromItems(data.Items)
	if errors.Is(err, errNoAnnoluxPlan) {
		return errPaddleEventIgnored
	}
	if err != nil {
		return err
	}
	if data.BillingPeriod == nil {
		return errors.New("completed Annolux transaction has no billing period")
	}
	intentID := checkoutIntentID(data.CustomData)
	if data.Origin == "web" && intentID == "" {
		return errors.New("completed checkout transaction has no intent")
	}
	_, _, _, err = service.accounts.ApplyCompletedTransaction(ctx, accounts.CompletedTransaction{
		TransactionID: data.ID, SubscriptionID: data.SubscriptionID, CustomerID: data.CustomerID,
		CheckoutIntentID: intentID, Origin: data.Origin, Plan: plan,
		PeriodStart: data.BillingPeriod.StartsAt, PeriodEnd: data.BillingPeriod.EndsAt, OccurredAt: event.OccurredAt,
	})
	return err
}

func (service *BillingService) processSubscription(ctx context.Context, event paddleEventEnvelope) error {
	var data struct {
		ID              string               `json:"id"`
		Status          string               `json:"status"`
		CustomerID      string               `json:"customer_id"`
		BillingPeriod   *paddleBillingPeriod `json:"current_billing_period"`
		CustomData      map[string]any       `json:"custom_data"`
		Items           []paddleItem         `json:"items"`
		ScheduledChange *struct {
			Action string `json:"action"`
		} `json:"scheduled_change"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil || (data.BillingPeriod == nil && data.Status != "paused" && data.Status != "canceled") {
		return errors.New("subscription payload is incomplete")
	}
	plan, err := service.planFromItems(data.Items)
	if errors.Is(err, errNoAnnoluxPlan) {
		return errPaddleEventIgnored
	}
	if err != nil {
		return err
	}
	cancelAtPeriodEnd := data.ScheduledChange != nil && data.ScheduledChange.Action == "cancel"
	var periodStart, periodEnd time.Time
	if data.BillingPeriod != nil {
		periodStart, periodEnd = data.BillingPeriod.StartsAt, data.BillingPeriod.EndsAt
	}
	_, _, err = service.accounts.ApplySubscriptionUpdate(ctx, accounts.SubscriptionUpdate{
		SubscriptionID: data.ID, CustomerID: data.CustomerID, CheckoutIntentID: checkoutIntentID(data.CustomData),
		EventKey: event.EventID, Plan: plan, Status: data.Status, PeriodStart: periodStart, PeriodEnd: periodEnd,
		CancelAtPeriodEnd: cancelAtPeriodEnd, OccurredAt: event.OccurredAt,
	})
	return err
}

func (service *BillingService) processAdjustment(ctx context.Context, event paddleEventEnvelope) error {
	var data struct {
		ID            string `json:"id"`
		Action        string `json:"action"`
		Status        string `json:"status"`
		TransactionID string `json:"transaction_id"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return err
	}
	if data.Status != "approved" || (data.Action != "refund" && data.Action != "credit" && data.Action != "chargeback") {
		return nil
	}
	_, _, err := service.accounts.ApplyApprovedAdjustment(ctx, data.TransactionID, data.ID, data.Action)
	return err
}

func sanitizeEventError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 1024 {
		message = message[:1024]
	}
	return message
}
