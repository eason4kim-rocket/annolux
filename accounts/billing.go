package accounts

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func validPlan(plan string) bool { return plan == "pro" || plan == "scale" }

func planCredits(plan string) int64 {
	if plan == "pro" {
		return 20_000
	}
	if plan == "scale" {
		return 100_000
	}
	return 0
}

func (store *Store) CreateCheckoutIntent(ctx context.Context, accountID, plan, termsVersion, refundVersion string, ttl time.Duration) (CheckoutIntent, error) {
	if ctx == nil || accountID == "" || !validPlan(plan) || strings.TrimSpace(termsVersion) == "" || strings.TrimSpace(refundVersion) == "" || len(termsVersion) > 64 || len(refundVersion) > 64 || ttl <= 0 || ttl > time.Hour {
		return CheckoutIntent{}, ErrInvalidInput
	}
	id, err := store.newID("chk_")
	if err != nil {
		return CheckoutIntent{}, err
	}
	now := store.now().UTC().Round(0)
	intent := CheckoutIntent{ID: id, AccountID: accountID, Plan: plan, TermsVersion: termsVersion, RefundVersion: refundVersion, State: "pending", ExpiresAt: now.Add(ttl), CreatedAt: now, UpdatedAt: now}
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
		if _, err := tx.ExecContext(ctx, `UPDATE checkout_intents SET state='expired',updated_at=? WHERE account_id=? AND state='pending' AND expires_at<=?`, now.Unix(), accountID, now.Unix()); err != nil {
			return err
		}
		var current int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM paddle_subscriptions WHERE account_id=? AND is_current=1 AND (status!='canceled' OR current_period_end>?) LIMIT 1`, accountID, now.Unix()).Scan(&current)
		if err == nil {
			return ErrConflict
		}
		if !isNoRows(err) {
			return err
		}
		var pending int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM checkout_intents WHERE account_id=? AND state='pending'`, accountID).Scan(&pending); err != nil {
			return err
		}
		if pending > 0 {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET terms_version=?,refund_version=?,updated_at=? WHERE id=?`, termsVersion, refundVersion, now.Unix(), accountID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO checkout_intents(id,account_id,plan,terms_version,refund_version,state,paddle_transaction_id,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,'pending','',?,?,?)`,
			intent.ID, intent.AccountID, intent.Plan, intent.TermsVersion, intent.RefundVersion, intent.ExpiresAt.Unix(), now.Unix(), now.Unix())
		return translateConstraint(err)
	})
	return intent, err
}

func (store *Store) GetCheckoutIntent(ctx context.Context, intentID string) (CheckoutIntent, error) {
	if store == nil || store.db == nil || ctx == nil || intentID == "" {
		return CheckoutIntent{}, ErrInvalidInput
	}
	return scanCheckoutIntent(store.db.QueryRowContext(ctx, `SELECT id,account_id,plan,terms_version,refund_version,state,paddle_transaction_id,expires_at,created_at,updated_at FROM checkout_intents WHERE id=?`, intentID))
}

func scanCheckoutIntent(row interface{ Scan(...any) error }) (CheckoutIntent, error) {
	var intent CheckoutIntent
	var expiresAt, createdAt, updatedAt int64
	err := row.Scan(&intent.ID, &intent.AccountID, &intent.Plan, &intent.TermsVersion, &intent.RefundVersion, &intent.State, &intent.PaddleTransactionID, &expiresAt, &createdAt, &updatedAt)
	if isNoRows(err) {
		return CheckoutIntent{}, ErrNotFound
	}
	if err != nil {
		return CheckoutIntent{}, err
	}
	intent.ExpiresAt, intent.CreatedAt, intent.UpdatedAt = unixTime(expiresAt), unixTime(createdAt), unixTime(updatedAt)
	return intent, nil
}

func (store *Store) PaddlePortalTarget(ctx context.Context, accountID string) (string, string, error) {
	if store == nil || store.db == nil || ctx == nil || accountID == "" {
		return "", "", ErrInvalidInput
	}
	var customerID, subscriptionID string
	err := store.db.QueryRowContext(ctx, `
		SELECT c.paddle_customer_id,COALESCE(s.paddle_subscription_id,'')
		FROM paddle_customers c LEFT JOIN paddle_subscriptions s ON s.account_id=c.account_id AND s.is_current=1
		WHERE c.account_id=?`, accountID).Scan(&customerID, &subscriptionID)
	if isNoRows(err) {
		return "", "", ErrNotFound
	}
	return customerID, subscriptionID, err
}

func (store *Store) CurrentSubscription(ctx context.Context, accountID string) (PaddleSubscription, error) {
	if store == nil || store.db == nil || ctx == nil || accountID == "" {
		return PaddleSubscription{}, ErrInvalidInput
	}
	var subscription PaddleSubscription
	var periodStart, periodEnd, cancelAtPeriodEnd, current, lastEventAt int64
	err := store.db.QueryRowContext(ctx, `SELECT paddle_subscription_id,account_id,plan,status,current_period_start,current_period_end,cancel_at_period_end,is_current,last_event_at_ns FROM paddle_subscriptions WHERE account_id=? AND is_current=1`, accountID).
		Scan(&subscription.ID, &subscription.AccountID, &subscription.Plan, &subscription.Status, &periodStart, &periodEnd, &cancelAtPeriodEnd, &current, &lastEventAt)
	if isNoRows(err) {
		return PaddleSubscription{}, ErrNotFound
	}
	if err != nil {
		return PaddleSubscription{}, err
	}
	subscription.PeriodStart, subscription.PeriodEnd, subscription.LastEventAt = unixTime(periodStart), unixTime(periodEnd), paddleEventTime(lastEventAt)
	subscription.CancelAtPeriodEnd, subscription.Current = cancelAtPeriodEnd == 1, current == 1
	return subscription, nil
}

func (store *Store) StorePaddleEvent(ctx context.Context, event PaddleEvent) (PaddleEvent, bool, error) {
	if ctx == nil || !strings.HasPrefix(event.EventID, "evt_") || len(event.EventID) > 64 || event.EventType == "" || len(event.EventType) > 128 || event.OccurredAt.IsZero() || len(event.Payload) == 0 || len(event.Payload) > 1<<20 || !json.Valid(event.Payload) {
		return PaddleEvent{}, false, ErrInvalidInput
	}
	event.OccurredAt = event.OccurredAt.UTC().Round(0)
	now := store.now().UTC().Round(0)
	created := false
	err := store.withWriteTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanPaddleEvent(tx.QueryRowContext(ctx, `SELECT event_id,event_type,occurred_at_ns,payload,state,attempts,last_error,created_at,updated_at FROM paddle_events WHERE event_id=?`, event.EventID))
		if err == nil {
			if existing.EventType != event.EventType || !existing.OccurredAt.Equal(event.OccurredAt) || !bytes.Equal(existing.Payload, event.Payload) {
				return ErrConflict
			}
			event = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		event.State, event.CreatedAt, event.UpdatedAt = "pending", now, now
		_, err = tx.ExecContext(ctx, `INSERT INTO paddle_events(event_id,event_type,occurred_at,occurred_at_ns,payload,state,attempts,last_error,created_at,updated_at) VALUES(?,?,?,?,?,'pending',0,'',?,?)`,
			event.EventID, event.EventType, event.OccurredAt.Unix(), paddleEventTimestamp(event.OccurredAt), event.Payload, now.Unix(), now.Unix())
		created = err == nil
		return translateConstraint(err)
	})
	return event, created, err
}

func (store *Store) GetPaddleEvent(ctx context.Context, eventID string) (PaddleEvent, error) {
	if store == nil || store.db == nil || ctx == nil || !strings.HasPrefix(eventID, "evt_") {
		return PaddleEvent{}, ErrInvalidInput
	}
	return scanPaddleEvent(store.db.QueryRowContext(ctx, `SELECT event_id,event_type,occurred_at_ns,payload,state,attempts,last_error,created_at,updated_at FROM paddle_events WHERE event_id=?`, eventID))
}

func (store *Store) SetPaddleEventState(ctx context.Context, eventID, state, lastError string) error {
	if ctx == nil || eventID == "" || (state != "processed" && state != "failed" && state != "ignored" && state != "pending") || len(lastError) > 1024 {
		return ErrInvalidInput
	}
	now := store.now().UTC().Unix()
	return store.withWriteTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE paddle_events SET state=?,attempts=attempts+1,last_error=?,updated_at=? WHERE event_id=?`, state, lastError, now, eventID)
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

func (store *Store) PendingPaddleEvents(ctx context.Context, limit int) ([]PaddleEvent, error) {
	if store == nil || store.db == nil || ctx == nil || limit < 1 || limit > 1000 {
		return nil, ErrInvalidInput
	}
	rows, err := store.db.QueryContext(ctx, `SELECT event_id,event_type,occurred_at_ns,payload,state,attempts,last_error,created_at,updated_at FROM paddle_events WHERE state IN ('pending','failed') ORDER BY occurred_at_ns,event_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []PaddleEvent
	for rows.Next() {
		event, err := scanPaddleEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanPaddleEvent(row interface{ Scan(...any) error }) (PaddleEvent, error) {
	var event PaddleEvent
	var occurredAt, createdAt, updatedAt int64
	err := row.Scan(&event.EventID, &event.EventType, &occurredAt, &event.Payload, &event.State, &event.Attempts, &event.LastError, &createdAt, &updatedAt)
	if isNoRows(err) {
		return PaddleEvent{}, ErrNotFound
	}
	if err != nil {
		return PaddleEvent{}, err
	}
	event.OccurredAt, event.CreatedAt, event.UpdatedAt = paddleEventTime(occurredAt), unixTime(createdAt), unixTime(updatedAt)
	return event, nil
}

func (store *Store) ApplyCompletedTransaction(ctx context.Context, input CompletedTransaction) (string, int64, bool, error) {
	if ctx == nil || !strings.HasPrefix(input.TransactionID, "txn_") || !strings.HasPrefix(input.SubscriptionID, "sub_") || !validCompletedTransactionOrigin(input.Origin) || !validPlan(input.Plan) || input.PeriodStart.IsZero() || !input.PeriodEnd.After(input.PeriodStart) || input.OccurredAt.IsZero() ||
		(input.Origin == "web" && input.CheckoutIntentID == "") {
		return "", 0, false, ErrInvalidInput
	}
	grantID, err := store.newID("grant_")
	if err != nil {
		return "", 0, false, err
	}
	ledgerID, err := store.newID("led_")
	if err != nil {
		return "", 0, false, err
	}
	now := store.now().UTC().Round(0)
	var accountID string
	var remaining int64
	created := false
	err = store.withWriteTx(ctx, func(tx *sql.Tx) error {
		intentID := input.CheckoutIntentID
		if input.Origin == "subscription_recurring" {
			// Paddle copies subscription custom_data to renewal transactions.
			// The original checkout intent is therefore attribution, not a new
			// authorization to consume or rebind that one-time intent.
			intentID = ""
		}
		resolved, err := resolvePaddleAccountTx(ctx, tx, intentID, input.CustomerID, input.SubscriptionID)
		if err != nil {
			return err
		}
		accountID = resolved
		var accountStatus AccountStatus
		if err := tx.QueryRowContext(ctx, `SELECT status FROM accounts WHERE id=?`, accountID).Scan(&accountStatus); err != nil {
			return err
		}
		if accountStatus != AccountActive {
			return ErrSuspended
		}
		if input.Origin == "web" {
			intent, err := scanCheckoutIntent(tx.QueryRowContext(ctx, `SELECT id,account_id,plan,terms_version,refund_version,state,paddle_transaction_id,expires_at,created_at,updated_at FROM checkout_intents WHERE id=?`, input.CheckoutIntentID))
			if err != nil || intent.AccountID != accountID || intent.Plan != input.Plan {
				return ErrConflict
			}
			switch intent.State {
			case "pending":
				if input.OccurredAt.After(intent.ExpiresAt) {
					return ErrExpired
				}
			case "completed":
				if intent.PaddleTransactionID != input.TransactionID {
					return ErrConflict
				}
			default:
				return ErrConflict
			}
		} else {
			// Renewals are valid only for the already-bound subscription and
			// cannot silently switch plans. A first purchase must carry the
			// checkout intent created by the authenticated account.
			subscriptionAccount, subscriptionPlan, err := paddleSubscriptionBindingTx(ctx, tx, input.SubscriptionID)
			if err != nil {
				return err
			}
			if subscriptionAccount != accountID || subscriptionPlan != input.Plan {
				return ErrConflict
			}
		}
		if err := upsertPaddleCustomerTx(ctx, tx, accountID, input.CustomerID, now); err != nil {
			return err
		}
		var existingAccount, existingGrant string
		var lastEventAt int64
		err = tx.QueryRowContext(ctx, `SELECT account_id,COALESCE(credit_grant_id,''),last_event_at FROM paddle_transactions WHERE paddle_transaction_id=?`, input.TransactionID).
			Scan(&existingAccount, &existingGrant, &lastEventAt)
		if err == nil {
			if existingAccount != accountID {
				return ErrConflict
			}
			remaining, err = availableCreditsTx(ctx, tx, accountID, now)
			return err
		}
		if !isNoRows(err) {
			return err
		}
		externalRef := "paddle:transaction:" + input.TransactionID
		units := planCredits(input.Plan)
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_grants(id,account_id,source,original_units,remaining_units,starts_at,expires_at,external_ref,created_at) VALUES(?,?,'subscription',?,?,?,?,?,?)`,
			grantID, accountID, units, units, input.PeriodStart.UTC().Unix(), input.PeriodEnd.UTC().Unix(), externalRef, now.Unix()); err != nil {
			return translateConstraint(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(id,account_id,kind,units,grant_id,reservation_id,external_ref,created_at) VALUES(?,?,'grant',?,?,NULL,?,?)`,
			ledgerID, accountID, units, grantID, externalRef, now.Unix()); err != nil {
			return translateConstraint(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO paddle_transactions(paddle_transaction_id,account_id,paddle_subscription_id,plan,status,period_start,period_end,credit_grant_id,last_event_at,last_event_at_ns,created_at,updated_at) VALUES(?,?,?,?,'completed',?,?,?,?,?,?,?)`,
			input.TransactionID, accountID, input.SubscriptionID, input.Plan, input.PeriodStart.UTC().Unix(), input.PeriodEnd.UTC().Unix(), grantID, input.OccurredAt.UTC().Unix(), paddleEventTimestamp(input.OccurredAt), now.Unix(), now.Unix()); err != nil {
			return translateConstraint(err)
		}
		if input.Origin == "web" {
			if _, err := tx.ExecContext(ctx, `UPDATE checkout_intents SET state='completed',paddle_transaction_id=?,updated_at=? WHERE id=?`, input.TransactionID, now.Unix(), input.CheckoutIntentID); err != nil {
				return err
			}
		}
		remaining, err = availableCreditsTx(ctx, tx, accountID, now)
		created = true
		return err
	})
	return accountID, remaining, created, err
}

func (store *Store) ApplySubscriptionUpdate(ctx context.Context, input SubscriptionUpdate) (string, bool, error) {
	missingPeriod := input.PeriodStart.IsZero() && input.PeriodEnd.IsZero()
	validPeriod := !input.PeriodStart.IsZero() && input.PeriodEnd.After(input.PeriodStart)
	if ctx == nil || !strings.HasPrefix(input.SubscriptionID, "sub_") || !strings.HasPrefix(input.CustomerID, "ctm_") || len(input.EventKey) > 128 || !validPlan(input.Plan) || !validSubscriptionStatus(input.Status) || (!validPeriod && !(missingPeriod && (input.Status == "paused" || input.Status == "canceled"))) || input.OccurredAt.IsZero() {
		return "", false, ErrInvalidInput
	}
	now := store.now().UTC().Round(0)
	var accountID string
	applied := false
	err := store.withWriteTx(ctx, func(tx *sql.Tx) error {
		resolved, err := resolvePaddleAccountTx(ctx, tx, input.CheckoutIntentID, input.CustomerID, input.SubscriptionID)
		if err != nil {
			return err
		}
		accountID = resolved
		if input.CheckoutIntentID != "" {
			intent, err := scanCheckoutIntent(tx.QueryRowContext(ctx, `SELECT id,account_id,plan,terms_version,refund_version,state,paddle_transaction_id,expires_at,created_at,updated_at FROM checkout_intents WHERE id=?`, input.CheckoutIntentID))
			if err != nil || intent.AccountID != accountID || intent.Plan != input.Plan {
				return ErrConflict
			}
			switch intent.State {
			case "pending":
				if input.OccurredAt.After(intent.ExpiresAt) {
					return ErrExpired
				}
			case "completed":
				var transactionSubscription, transactionPlan string
				if err := tx.QueryRowContext(ctx, `SELECT paddle_subscription_id,plan FROM paddle_transactions WHERE paddle_transaction_id=? AND account_id=?`, intent.PaddleTransactionID, accountID).Scan(&transactionSubscription, &transactionPlan); err != nil || transactionSubscription != input.SubscriptionID || transactionPlan != input.Plan {
					return ErrConflict
				}
			default:
				return ErrConflict
			}
		} else {
			// Renewal/lifecycle events may omit custom_data only after the exact
			// subscription has already been bound. A known customer alone is not
			// enough to establish a new subscription or switch plans.
			var existingAccount, existingPlan string
			if err := tx.QueryRowContext(ctx, `SELECT account_id,plan FROM paddle_subscriptions WHERE paddle_subscription_id=?`, input.SubscriptionID).Scan(&existingAccount, &existingPlan); err != nil || existingAccount != accountID || existingPlan != input.Plan {
				return ErrConflict
			}
		}
		if err := upsertPaddleCustomerTx(ctx, tx, accountID, input.CustomerID, now); err != nil {
			return err
		}
		var existingAccount, existingPlan string
		var existingPeriodStart, existingPeriodEnd, lastEventAt int64
		var lastEventKey string
		err = tx.QueryRowContext(ctx, `SELECT account_id,plan,current_period_start,current_period_end,last_event_at_ns,last_event_key FROM paddle_subscriptions WHERE paddle_subscription_id=?`, input.SubscriptionID).Scan(&existingAccount, &existingPlan, &existingPeriodStart, &existingPeriodEnd, &lastEventAt, &lastEventKey)
		if err == nil {
			if existingAccount != accountID || existingPlan != input.Plan {
				return ErrConflict
			}
			incomingTimestamp := paddleEventTimestamp(input.OccurredAt)
			if lastEventAt > incomingTimestamp || (lastEventAt == incomingTimestamp && lastEventKey >= input.EventKey) {
				return nil
			}
			periodStart, periodEnd := input.PeriodStart.UTC().Unix(), input.PeriodEnd.UTC().Unix()
			if missingPeriod {
				periodStart, periodEnd = existingPeriodStart, existingPeriodEnd
			}
			// Historical subscriptions remain historical. A delayed event for a
			// retired subscription may refresh its audit record, but must not steal
			// the account's single-current slot from its replacement.
			_, err = tx.ExecContext(ctx, `UPDATE paddle_subscriptions SET status=?,current_period_start=?,current_period_end=?,cancel_at_period_end=?,is_current=CASE WHEN ?='canceled' THEN 0 ELSE is_current END,last_event_at=?,last_event_at_ns=?,last_event_key=?,updated_at=? WHERE paddle_subscription_id=?`,
				input.Status, periodStart, periodEnd, boolInt(input.CancelAtPeriodEnd), input.Status, input.OccurredAt.UTC().Unix(), incomingTimestamp, input.EventKey, now.Unix(), input.SubscriptionID)
			applied = err == nil
			return translateConstraint(err)
		}
		if !isNoRows(err) {
			return err
		}
		if missingPeriod {
			// Paddle may send null current_billing_period for terminal or paused
			// lifecycle events. Such an event can update only an exact subscription
			// that is already bound locally; it cannot establish a new binding.
			return ErrNotFound
		}
		var other string
		// Defensively retire legacy rows that represented a scheduled cancellation
		// as status=canceled while still current. Current Paddle semantics keep the
		// status active until cancellation takes effect, and the effective event is
		// retired immediately above.
		if _, err := tx.ExecContext(ctx, `UPDATE paddle_subscriptions SET is_current=0,updated_at=? WHERE account_id=? AND is_current=1 AND status='canceled' AND current_period_end<=?`,
			now.Unix(), accountID, input.OccurredAt.UTC().Unix()); err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT paddle_subscription_id FROM paddle_subscriptions WHERE account_id=? AND is_current=1 LIMIT 1`, accountID).Scan(&other)
		if err == nil && other != input.SubscriptionID {
			return ErrConflict
		}
		if !isNoRows(err) && err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO paddle_subscriptions(paddle_subscription_id,account_id,plan,status,current_period_start,current_period_end,cancel_at_period_end,is_current,last_event_at,last_event_at_ns,last_event_key,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			input.SubscriptionID, accountID, input.Plan, input.Status, input.PeriodStart.UTC().Unix(), input.PeriodEnd.UTC().Unix(), boolInt(input.CancelAtPeriodEnd), boolInt(input.Status != "canceled"), input.OccurredAt.UTC().Unix(), paddleEventTimestamp(input.OccurredAt), input.EventKey, now.Unix(), now.Unix())
		applied = err == nil
		return translateConstraint(err)
	})
	return accountID, applied, err
}

func (store *Store) ApplyApprovedAdjustment(ctx context.Context, transactionID, adjustmentID, action string) (string, int64, error) {
	if ctx == nil || !strings.HasPrefix(transactionID, "txn_") || !strings.HasPrefix(adjustmentID, "adj_") || (action != "refund" && action != "credit" && action != "chargeback") {
		return "", 0, ErrInvalidInput
	}
	now := store.now().UTC().Round(0)
	var accountID string
	var revoked int64
	err := store.withWriteTx(ctx, func(tx *sql.Tx) error {
		var grantID string
		if err := tx.QueryRowContext(ctx, `SELECT account_id,COALESCE(credit_grant_id,'') FROM paddle_transactions WHERE paddle_transaction_id=?`, transactionID).Scan(&accountID, &grantID); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		externalRef := "paddle:adjustment:" + adjustmentID
		var existing int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM credit_ledger WHERE external_ref=?`, externalRef).Scan(&existing)
		if err == nil {
			return nil
		}
		if !isNoRows(err) {
			return err
		}
		if grantID != "" {
			if err := tx.QueryRowContext(ctx, `SELECT remaining_units FROM credit_grants WHERE id=?`, grantID).Scan(&revoked); err != nil {
				return err
			}
			rows, err := tx.QueryContext(ctx, `SELECT DISTINCT r.id
				FROM credit_reservations r JOIN credit_reservation_allocations a ON a.reservation_id=r.id
				WHERE r.state='pending' AND a.grant_id=? ORDER BY r.id`, grantID)
			if err != nil {
				return err
			}
			var pending []string
			for rows.Next() {
				var reservationID string
				if err := rows.Scan(&reservationID); err != nil {
					rows.Close()
					return err
				}
				pending = append(pending, reservationID)
			}
			if err := rows.Close(); err != nil {
				return err
			}
			for _, reservationID := range pending {
				allocationRows, err := tx.QueryContext(ctx, `SELECT grant_id,units FROM credit_reservation_allocations WHERE reservation_id=? ORDER BY grant_id`, reservationID)
				if err != nil {
					return err
				}
				type allocation struct {
					grantID string
					units   int64
				}
				var allocations []allocation
				for allocationRows.Next() {
					var value allocation
					if err := allocationRows.Scan(&value.grantID, &value.units); err != nil {
						allocationRows.Close()
						return err
					}
					allocations = append(allocations, value)
				}
				if err := allocationRows.Close(); err != nil {
					return err
				}
				for _, value := range allocations {
					if value.grantID == grantID {
						revoked += value.units
						continue
					}
					result, err := tx.ExecContext(ctx, `UPDATE credit_grants SET remaining_units=remaining_units+? WHERE id=? AND remaining_units+?<=original_units`, value.units, value.grantID, value.units)
					if err != nil {
						return err
					}
					affected, _ := result.RowsAffected()
					if affected != 1 {
						return ErrConflict
					}
				}
				result, err := tx.ExecContext(ctx, `UPDATE credit_reservations SET state='released',settled_at=? WHERE id=? AND state='pending'`, now.Unix(), reservationID)
				if err != nil {
					return err
				}
				affected, _ := result.RowsAffected()
				if affected != 1 {
					return ErrConflict
				}
			}
			if revoked > 0 {
				ledgerID, err := store.newID("led_")
				if err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `UPDATE credit_grants SET remaining_units=0 WHERE id=?`, grantID); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(id,account_id,kind,units,grant_id,reservation_id,external_ref,created_at) VALUES(?,?,'revocation',?,?,NULL,?,?)`,
					ledgerID, accountID, -revoked, grantID, externalRef, now.Unix()); err != nil {
					return translateConstraint(err)
				}
			}
		}
		if action == "chargeback" {
			if _, err := tx.ExecContext(ctx, `UPDATE accounts SET status='suspended',updated_at=? WHERE id=?`, now.Unix(), accountID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE api_keys SET revoked_at=COALESCE(revoked_at,?) WHERE account_id=?`, now.Unix(), accountID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,?) WHERE account_id=?`, now.Unix(), accountID); err != nil {
				return err
			}
		}
		return nil
	})
	return accountID, revoked, err
}

func resolvePaddleAccountTx(ctx context.Context, tx *sql.Tx, intentID, customerID, subscriptionID string) (string, error) {
	var accountID string
	if intentID != "" {
		err := tx.QueryRowContext(ctx, `SELECT account_id FROM checkout_intents WHERE id=?`, intentID).Scan(&accountID)
		if err == nil {
			return accountID, nil
		}
		if !isNoRows(err) {
			return "", err
		}
	}
	if subscriptionID != "" {
		err := tx.QueryRowContext(ctx, `SELECT account_id FROM paddle_subscriptions WHERE paddle_subscription_id=?`, subscriptionID).Scan(&accountID)
		if err == nil {
			return accountID, nil
		}
		if !isNoRows(err) {
			return "", err
		}
	}
	if customerID != "" {
		err := tx.QueryRowContext(ctx, `SELECT account_id FROM paddle_customers WHERE paddle_customer_id=?`, customerID).Scan(&accountID)
		if err == nil {
			return accountID, nil
		}
		if !isNoRows(err) {
			return "", err
		}
	}
	return "", ErrNotFound
}

func paddleSubscriptionBindingTx(ctx context.Context, tx *sql.Tx, subscriptionID string) (string, string, error) {
	var accountID, plan string
	err := tx.QueryRowContext(ctx, `SELECT account_id,plan FROM paddle_subscriptions WHERE paddle_subscription_id=?`, subscriptionID).Scan(&accountID, &plan)
	if err == nil {
		return accountID, plan, nil
	}
	if !isNoRows(err) {
		return "", "", err
	}
	// Reconciliation lists transactions before subscriptions. The original
	// checkout transaction is also an authoritative subscription binding, so a
	// later recurring transaction can be repaired before the subscription list
	// pass creates paddle_subscriptions.
	err = tx.QueryRowContext(ctx, `SELECT account_id,plan FROM paddle_transactions WHERE paddle_subscription_id=? ORDER BY created_at,paddle_transaction_id LIMIT 1`, subscriptionID).Scan(&accountID, &plan)
	if isNoRows(err) {
		return "", "", ErrConflict
	}
	return accountID, plan, err
}

func validCompletedTransactionOrigin(value string) bool {
	return value == "web" || value == "subscription_recurring"
}

func paddleEventTimestamp(value time.Time) int64 {
	return value.UTC().UnixNano()
}

func paddleEventTime(value int64) time.Time {
	return time.Unix(0, value).UTC()
}

func upsertPaddleCustomerTx(ctx context.Context, tx *sql.Tx, accountID, customerID string, now time.Time) error {
	if !strings.HasPrefix(customerID, "ctm_") {
		return ErrInvalidInput
	}
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT account_id FROM paddle_customers WHERE paddle_customer_id=? OR account_id=? LIMIT 1`, customerID, accountID).Scan(&existing)
	if err == nil {
		if existing != accountID {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE paddle_customers SET updated_at=? WHERE paddle_customer_id=? AND account_id=?`, now.Unix(), customerID, accountID)
		return err
	}
	if !isNoRows(err) {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paddle_customers(paddle_customer_id,account_id,created_at,updated_at) VALUES(?,?,?,?)`, customerID, accountID, now.Unix(), now.Unix())
	return translateConstraint(err)
}

func validSubscriptionStatus(value string) bool {
	switch value {
	case "trialing", "active", "past_due", "paused", "canceled":
		return true
	default:
		return false
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
