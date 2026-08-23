package publicapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eason4kim-rocket/annolux/accounts"
)

const maximumReconcilePages = 100

// ReconcileStats summarizes one read-only Paddle API scan and idempotent local
// repair pass. "Applied" includes safe repeats whose local unique IDs already
// existed; the ledger remains the source of truth for actual new grants.
type ReconcileStats struct {
	Transactions  int `json:"transactions"`
	Subscriptions int `json:"subscriptions"`
	Adjustments   int `json:"adjustments"`
	Skipped       int `json:"skipped"`
}

// ReconcileSince repairs missed webhooks using Paddle list APIs. Transactions
// run first so their customer/subscription mapping exists before subscription
// and adjustment records are applied. All account-side operations are
// independently idempotent.
func (service *BillingService) ReconcileSince(ctx context.Context, since time.Time) (ReconcileStats, error) {
	if service == nil || ctx == nil || since.IsZero() || since.After(service.now().UTC().Add(time.Minute)) || since.Before(service.now().UTC().Add(-90*24*time.Hour)) {
		return ReconcileStats{}, errors.New("billing reconciliation requires a time within the last 90 days")
	}
	stats := ReconcileStats{}
	transactionURL, err := url.Parse(service.apiBase + "/transactions")
	if err != nil {
		return stats, err
	}
	query := transactionURL.Query()
	query.Set("status", "completed")
	query.Set("updated_at[GTE]", since.UTC().Format(time.RFC3339))
	query.Set("order_by", "updated_at[ASC]")
	query.Set("per_page", "30")
	transactionURL.RawQuery = query.Encode()
	if err := service.reconcileList(ctx, transactionURL.String(), "/transactions", func(raw json.RawMessage) error {
		var metadata struct {
			UpdatedAt time.Time    `json:"updated_at"`
			Origin    string       `json:"origin"`
			Items     []paddleItem `json:"items"`
		}
		if err := json.Unmarshal(raw, &metadata); err != nil || metadata.UpdatedAt.IsZero() {
			return errors.New("Paddle transaction list returned invalid data")
		}
		if metadata.Origin != "web" && metadata.Origin != "subscription_recurring" {
			stats.Skipped++
			return nil
		}
		if _, err := service.planFromItems(metadata.Items); err != nil {
			stats.Skipped++
			return nil
		}
		event := paddleEventEnvelope{EventType: "transaction.completed", OccurredAt: metadata.UpdatedAt, Data: raw}
		if err := service.processCompletedTransaction(ctx, event); errors.Is(err, errPaddleEventIgnored) {
			stats.Skipped++
			return nil
		} else if err != nil {
			return err
		}
		stats.Transactions++
		return nil
	}); err != nil {
		return stats, err
	}

	if err := service.reconcileList(ctx, service.apiBase+"/subscriptions?per_page=200", "/subscriptions", func(raw json.RawMessage) error {
		var metadata struct {
			UpdatedAt     time.Time            `json:"updated_at"`
			Status        string               `json:"status"`
			BillingPeriod *paddleBillingPeriod `json:"current_billing_period"`
			Items         []paddleItem         `json:"items"`
		}
		if err := json.Unmarshal(raw, &metadata); err != nil || metadata.UpdatedAt.IsZero() {
			return errors.New("Paddle subscription list returned invalid data")
		}
		if metadata.UpdatedAt.Before(since) || (metadata.BillingPeriod == nil && metadata.Status != "paused" && metadata.Status != "canceled") {
			stats.Skipped++
			return nil
		}
		if _, err := service.planFromItems(metadata.Items); err != nil {
			stats.Skipped++
			return nil
		}
		event := paddleEventEnvelope{EventType: "subscription.updated", OccurredAt: metadata.UpdatedAt, Data: raw}
		if err := service.processSubscription(ctx, event); errors.Is(err, accounts.ErrNotFound) || errors.Is(err, errPaddleEventIgnored) {
			stats.Skipped++
			return nil
		} else if err != nil {
			return err
		}
		stats.Subscriptions++
		return nil
	}); err != nil {
		return stats, err
	}

	adjustmentURL := service.apiBase + "/adjustments?action=refund,credit,chargeback&status=approved&per_page=50"
	if err := service.reconcileList(ctx, adjustmentURL, "/adjustments", func(raw json.RawMessage) error {
		var metadata struct {
			UpdatedAt time.Time `json:"updated_at"`
		}
		if err := json.Unmarshal(raw, &metadata); err != nil || metadata.UpdatedAt.IsZero() {
			return errors.New("Paddle adjustment list returned invalid data")
		}
		if metadata.UpdatedAt.Before(since) {
			stats.Skipped++
			return nil
		}
		event := paddleEventEnvelope{EventType: "adjustment.updated", OccurredAt: metadata.UpdatedAt, Data: raw}
		if err := service.processAdjustment(ctx, event); errors.Is(err, accounts.ErrNotFound) {
			stats.Skipped++
			return nil
		} else if err != nil {
			return err
		}
		stats.Adjustments++
		return nil
	}); err != nil {
		return stats, err
	}
	return stats, nil
}

func (service *BillingService) reconcileList(ctx context.Context, endpoint, expectedPath string, apply func(json.RawMessage) error) error {
	next := endpoint
	for page := 0; next != ""; page++ {
		if page >= maximumReconcilePages {
			return errors.New("Paddle reconciliation exceeded the page limit")
		}
		if err := service.validatePaddlePageURL(next, expectedPath); err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+service.apiKey)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Skip-Count", "true")
		response, err := service.client.Do(request)
		if err != nil {
			return err
		}
		var envelope struct {
			Data []json.RawMessage `json:"data"`
			Meta struct {
				Pagination struct {
					Next    string `json:"next"`
					HasMore bool   `json:"has_more"`
				} `json:"pagination"`
			} `json:"meta"`
		}
		decodeErr := decodeBoundedJSON(response.Body, 8<<20, &envelope)
		closeErr := response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("Paddle reconciliation %s returned status %d", expectedPath, response.StatusCode)
		}
		if decodeErr != nil {
			return fmt.Errorf("decode Paddle reconciliation %s: %w", expectedPath, decodeErr)
		}
		if closeErr != nil {
			return closeErr
		}
		for _, raw := range envelope.Data {
			if err := apply(raw); err != nil {
				return err
			}
		}
		if !envelope.Meta.Pagination.HasMore {
			return nil
		}
		next = strings.TrimSpace(envelope.Meta.Pagination.Next)
		if next == "" {
			return errors.New("Paddle pagination claimed more data without a next URL")
		}
	}
	return nil
}

func (service *BillingService) validatePaddlePageURL(raw, expectedPath string) error {
	parsed, err := url.Parse(raw)
	base, baseErr := url.Parse(service.apiBase)
	if err != nil || baseErr != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Host != base.Host || parsed.Path != expectedPath {
		return errors.New("Paddle pagination returned an unsafe URL")
	}
	return nil
}
