package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/eason4kim-rocket/annolux/accounts"
	"github.com/eason4kim-rocket/annolux/publicapi"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("annolux-admin", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	databasePath := flags.String("db", envOr("ANNOLUX_ACCOUNTS_PATH", "/data/accounts.db"), "accounts.db path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	commandArguments := flags.Args()
	if len(commandArguments) == 0 {
		return usageError()
	}
	if commandArguments[0] == "verify-index" {
		if len(commandArguments) != 2 {
			return usageError()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		audit, err := auditIndex(ctx, commandArguments[1])
		if err != nil {
			return err
		}
		return writeJSON(audit)
	}
	// The query-log commands only need the log path, never account secrets.
	if commandArguments[0] == "querylog-stats" || commandArguments[0] == "querylog-expire" {
		return runQueryLogCommand(commandArguments)
	}
	secret, err := loadHMACSecret()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := accounts.Open(ctx, *databasePath, accounts.Config{HMACSecret: secret})
	if err != nil {
		return fmt.Errorf("open account store: %w", err)
	}
	defer store.Close()
	switch commandArguments[0] {
	case "metrics":
		if len(commandArguments) != 1 {
			return usageError()
		}
		metrics, err := store.Metrics(ctx)
		if err != nil {
			return err
		}
		return writeJSON(metrics)
	case "integrity":
		if len(commandArguments) != 1 {
			return usageError()
		}
		if err := store.IntegrityCheck(ctx); err != nil {
			return err
		}
		return writeJSON(map[string]any{"ok": true})
	case "snapshot":
		if len(commandArguments) != 2 {
			return usageError()
		}
		if err := store.Snapshot(ctx, commandArguments[1]); err != nil {
			return err
		}
		return writeJSON(map[string]any{"ok": true, "path": commandArguments[1]})
	case "account-status":
		if len(commandArguments) != 3 {
			return usageError()
		}
		status := accounts.AccountStatus(commandArguments[2])
		if status != accounts.AccountActive && status != accounts.AccountSuspended {
			return errors.New("status must be active or suspended")
		}
		if err := store.SetAccountStatus(ctx, commandArguments[1], status); err != nil {
			return err
		}
		return writeJSON(map[string]any{"ok": true, "account_id": commandArguments[1], "status": status})
	case "close-account":
		if len(commandArguments) != 3 || commandArguments[2] != "CONFIRM" {
			return errors.New("close-account requires ACCOUNT_ID CONFIRM")
		}
		if err := store.CloseAccount(ctx, commandArguments[1]); err != nil {
			return err
		}
		return writeJSON(map[string]any{"ok": true, "account_id": commandArguments[1], "status": accounts.AccountClosed})
	case "revoke-key":
		if len(commandArguments) != 3 {
			return usageError()
		}
		if err := store.RevokeAPIKey(ctx, commandArguments[1], commandArguments[2]); err != nil {
			return err
		}
		return writeJSON(map[string]any{"ok": true})
	case "usage":
		if len(commandArguments) != 2 {
			return usageError()
		}
		summary, err := store.CreditSummary(ctx, commandArguments[1])
		if err != nil {
			return err
		}
		return writeJSON(summary)
	case "ledger":
		if len(commandArguments) < 2 || len(commandArguments) > 3 {
			return usageError()
		}
		limit, err := optionalLimit(commandArguments, 2, 100)
		if err != nil {
			return err
		}
		entries, err := store.LedgerEntries(ctx, commandArguments[1], limit)
		if err != nil {
			return err
		}
		return writeJSON(entries)
	case "expire-credits":
		if len(commandArguments) != 1 {
			return usageError()
		}
		count, err := store.ExpireCredits(ctx)
		if err != nil {
			return err
		}
		return writeJSON(map[string]any{"expired_grants": count})
	case "release-stale-reservations":
		if len(commandArguments) > 2 {
			return usageError()
		}
		age := 5 * time.Minute
		if len(commandArguments) == 2 {
			parsed, err := time.ParseDuration(commandArguments[1])
			if err != nil {
				return errors.New("stale reservation age must be a duration")
			}
			age = parsed
		}
		count, err := store.ReleaseStaleReservations(ctx, age)
		if err != nil {
			return err
		}
		return writeJSON(map[string]any{"released_reservations": count})
	case "refund", "chargeback":
		if len(commandArguments) != 3 {
			return usageError()
		}
		action := commandArguments[0]
		accountID, revoked, err := store.ApplyApprovedAdjustment(ctx, commandArguments[1], commandArguments[2], action)
		if err != nil {
			return err
		}
		return writeJSON(map[string]any{"ok": true, "account_id": accountID, "revoked_credits": revoked, "action": action})
	case "paddle-events":
		limit, err := optionalLimit(commandArguments, 1, 100)
		if err != nil || len(commandArguments) > 2 {
			return usageError()
		}
		events, err := store.PendingPaddleEvents(ctx, limit)
		if err != nil {
			return err
		}
		metadata := make([]map[string]any, 0, len(events))
		for _, event := range events {
			metadata = append(metadata, map[string]any{"event_id": event.EventID, "event_type": event.EventType, "occurred_at": event.OccurredAt, "state": event.State, "attempts": event.Attempts, "last_error": event.LastError})
		}
		return writeJSON(metadata)
	case "replay-paddle":
		limit, err := optionalLimit(commandArguments, 1, 100)
		if err != nil || len(commandArguments) > 2 {
			return usageError()
		}
		billing, err := publicapi.NewBillingService(loadBillingConfig(), store)
		if err != nil {
			return err
		}
		processed, err := billing.ReprocessPending(ctx, limit)
		if err != nil {
			return err
		}
		return writeJSON(map[string]any{"processed": processed})
	case "reconcile-paddle":
		if len(commandArguments) > 2 {
			return usageError()
		}
		since := time.Now().UTC().Add(-48 * time.Hour)
		if len(commandArguments) == 2 {
			parsed, err := time.Parse(time.RFC3339, commandArguments[1])
			if err != nil {
				return errors.New("reconciliation start must be RFC3339")
			}
			since = parsed
		}
		billing, err := publicapi.NewBillingService(loadBillingConfig(), store)
		if err != nil {
			return err
		}
		stats, err := billing.ReconcileSince(ctx, since)
		if err != nil {
			return err
		}
		return writeJSON(map[string]any{"since": since, "completed_at": time.Now().UTC(), "stats": stats})
	default:
		return usageError()
	}
}

func loadHMACSecret() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("ANNOLUX_ACCOUNT_HMAC_SECRET"))
	secret, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		secret, err = base64.StdEncoding.DecodeString(raw)
	}
	if err != nil || len(secret) < 32 {
		return nil, errors.New("ANNOLUX_ACCOUNT_HMAC_SECRET must be base64 for at least 32 random bytes")
	}
	return secret, nil
}

func loadBillingConfig() publicapi.BillingConfig {
	return publicapi.BillingConfig{
		Environment: envOr("ANNOLUX_PADDLE_ENVIRONMENT", "sandbox"),
		APIKey:      os.Getenv("ANNOLUX_PADDLE_API_KEY"), ClientToken: os.Getenv("ANNOLUX_PADDLE_CLIENT_TOKEN"),
		WebhookSecret: os.Getenv("ANNOLUX_PADDLE_WEBHOOK_SECRET"),
		ProPriceID:    os.Getenv("ANNOLUX_PADDLE_PRO_PRICE_ID"), ScalePriceID: os.Getenv("ANNOLUX_PADDLE_SCALE_PRICE_ID"),
		TermsVersion: envOr("ANNOLUX_TERMS_VERSION", "2026-08-16"), RefundVersion: envOr("ANNOLUX_REFUND_VERSION", "2026-08-16"),
		LiveEnabled: strings.EqualFold(os.Getenv("ANNOLUX_PADDLE_LIVE_ENABLED"), "true") && strings.EqualFold(os.Getenv("ANNOLUX_PADDLE_LIVE_APPROVED"), "true"),
	}
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func optionalLimit(arguments []string, index, fallback int) (int, error) {
	if len(arguments) <= index {
		return fallback, nil
	}
	value, err := strconv.Atoi(arguments[index])
	if err != nil || value < 1 || value > 1000 {
		return 0, errors.New("limit must be an integer between 1 and 1000")
	}
	return value, nil
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func usageError() error {
	return errors.New("usage: annolux-admin [-db PATH] verify-index PATH | querylog-stats PATH | querylog-expire PATH [DAYS] | metrics | integrity | snapshot PATH | account-status ACCOUNT active|suspended | close-account ACCOUNT CONFIRM | revoke-key ACCOUNT KEY | usage ACCOUNT | ledger ACCOUNT [LIMIT] | expire-credits | release-stale-reservations [AGE] | refund TXN ADJ | chargeback TXN ADJ | paddle-events [LIMIT] | replay-paddle [LIMIT] | reconcile-paddle [RFC3339]")
}
