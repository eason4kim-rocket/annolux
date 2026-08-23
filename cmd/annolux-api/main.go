package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/eason4kim-rocket/annolux/accounts"
	"github.com/eason4kim-rocket/annolux/hostedsearch"
	"github.com/eason4kim-rocket/annolux/publicapi"
	"github.com/eason4kim-rocket/annolux/querylog"
	"github.com/eason4kim-rocket/annolux/searchfuse"
	"github.com/eason4kim-rocket/annolux/searchindex"
)

var version = "dev"

type runtimeConfig struct {
	ListenAddr      string
	Origin          string
	IndexPath       string
	AccountsPath    string
	QueryLogPath    string
	HMACSecret      []byte
	OAuth           publicapi.OAuthConfig
	Billing         publicapi.BillingConfig
	TermsVersion    string
	RefundVersion   string
	MaxConcurrent   int
	DemoDailyBudget int
	Fusion          searchfuse.Config
}

func main() {
	if err := run(); err != nil {
		slog.Error("annolux-api stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	config, err := loadRuntimeConfig(os.LookupEnv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	index, err := searchindex.OpenReadOnly(config.IndexPath)
	if err != nil {
		return fmt.Errorf("open search index: %w", err)
	}
	defer index.Close()
	searchService, err := hostedsearch.NewWithFusion(index, config.Fusion)
	if err != nil {
		return fmt.Errorf("create search service: %w", err)
	}
	if config.Fusion.Enabled {
		slog.Info("tier-1 fusion enabled", "rrf_k", config.Fusion.RRFK, "window", config.Fusion.Window)
	}
	accountStore, err := accounts.Open(ctx, config.AccountsPath, accounts.Config{HMACSecret: config.HMACSecret, TermsVersion: config.TermsVersion, RefundVersion: config.RefundVersion})
	if err != nil {
		return fmt.Errorf("open account store: %w", err)
	}
	defer accountStore.Close()
	go recoverCreditReservations(ctx, accountStore)
	// Query logging is telemetry and must never gate the product: a store
	// that cannot open leaves the API running without a log, and a logger
	// that cannot write drops records instead of failing requests.
	// "off" exists so an operator can measure the API with logging disabled;
	// an empty variable still means the default path, not silence.
	var queryLogger *querylog.Logger
	if config.QueryLogPath != "" && !strings.EqualFold(config.QueryLogPath, "off") {
		queryLogStore, logErr := querylog.Open(config.QueryLogPath)
		if logErr != nil {
			slog.Error("query log disabled", "path", config.QueryLogPath, "error", logErr)
		} else {
			queryLogger = querylog.NewLogger(queryLogStore, slog.Default(), querylog.DefaultRetention)
			defer func() {
				if closeErr := queryLogger.Close(); closeErr != nil {
					slog.Error("query log close failed", "error", closeErr)
				}
				if dropped := queryLogger.Dropped(); dropped > 0 {
					slog.Warn("query log dropped records", "count", dropped)
				}
			}()
		}
	}
	api, err := publicapi.New(publicapi.Config{
		Origin: config.Origin, Accounts: accountStore, Search: searchService, OAuth: config.OAuth,
		Billing: config.Billing, Version: version, MaxConcurrent: config.MaxConcurrent,
		DemoDailyBudget: config.DemoDailyBudget,
		QueryLog:        queryLogger,
		Ready: func(readyContext context.Context) error {
			pages, err := index.CountPages(readyContext)
			if err != nil {
				return err
			}
			if pages == 0 {
				return errors.New("search index is empty")
			}
			return nil
		},
	})
	if err != nil {
		return fmt.Errorf("create public API: %w", err)
	}
	server := &http.Server{
		Addr: config.ListenAddr, Handler: api.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second,
		WriteTimeout: 40 * time.Second, IdleTimeout: 90 * time.Second,
		MaxHeaderBytes: 32 << 10,
		BaseContext:    func(net.Listener) context.Context { return ctx },
	}
	listener, err := net.Listen("tcp", config.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	errorChannel := make(chan error, 1)
	go func() { errorChannel <- server.Serve(listener) }()
	slog.Info("annolux-api listening", "address", config.ListenAddr, "version", version)
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	case err := <-errorChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func recoverCreditReservations(ctx context.Context, store *accounts.Store) {
	const staleAfter = 5 * time.Minute
	recover := func() {
		recoveryContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		count, err := store.ReleaseStaleReservations(recoveryContext, staleAfter)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("credit reservation recovery failed", "error", err)
		} else if count > 0 {
			slog.Warn("released stale credit reservations", "count", count)
		}
	}
	recover()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			recover()
		}
	}
}

type lookupEnv func(string) (string, bool)

func loadRuntimeConfig(lookup lookupEnv) (runtimeConfig, error) {
	if lookup == nil {
		return runtimeConfig{}, errors.New("environment lookup is required")
	}
	value := func(name, fallback string) string {
		if raw, ok := lookup(name); ok && strings.TrimSpace(raw) != "" {
			return strings.TrimSpace(raw)
		}
		return fallback
	}
	secretText := value("ANNOLUX_ACCOUNT_HMAC_SECRET", "")
	secret, err := base64.RawURLEncoding.DecodeString(secretText)
	if err != nil {
		secret, err = base64.StdEncoding.DecodeString(secretText)
	}
	if err != nil || len(secret) < 32 {
		return runtimeConfig{}, errors.New("ANNOLUX_ACCOUNT_HMAC_SECRET must be base64 for at least 32 random bytes")
	}
	config := runtimeConfig{
		ListenAddr:    value("ANNOLUX_LISTEN_ADDR", "127.0.0.1:8083"),
		Origin:        value("ANNOLUX_ORIGIN", "https://annolux.com"),
		IndexPath:     value("ANNOLUX_INDEX_PATH", "/data/index.db"),
		AccountsPath:  value("ANNOLUX_ACCOUNTS_PATH", "/data/accounts.db"),
		QueryLogPath:  value("ANNOLUX_QUERYLOG_PATH", "/data/querylog.db"),
		HMACSecret:    secret,
		TermsVersion:  value("ANNOLUX_TERMS_VERSION", accounts.DefaultTermsVersion),
		RefundVersion: value("ANNOLUX_REFUND_VERSION", accounts.DefaultRefundVersion),
		MaxConcurrent: 12,
	}
	if raw := value("ANNOLUX_MAX_CONCURRENT_SEARCH", "12"); raw != "12" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > 64 {
			return runtimeConfig{}, errors.New("ANNOLUX_MAX_CONCURRENT_SEARCH must be between 1 and 64")
		}
		config.MaxConcurrent = parsed
	}
	if raw := value("ANNOLUX_DEMO_DAILY_BUDGET", ""); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > 1000000 {
			return runtimeConfig{}, errors.New("ANNOLUX_DEMO_DAILY_BUDGET must be between 1 and 1000000")
		}
		config.DemoDailyBudget = parsed
	}
	apiPublicURL := strings.TrimRight(value("ANNOLUX_API_PUBLIC_URL", "https://api.annolux.com"), "/")
	config.OAuth = publicapi.OAuthConfig{
		Google: publicapi.OAuthClientConfig{
			ClientID: value("ANNOLUX_GOOGLE_CLIENT_ID", ""), ClientSecret: value("ANNOLUX_GOOGLE_CLIENT_SECRET", ""),
			RedirectURL: value("ANNOLUX_GOOGLE_REDIRECT_URL", apiPublicURL+"/api/v1/auth/google/callback"),
		},
		GitHub: publicapi.OAuthClientConfig{
			ClientID: value("ANNOLUX_GITHUB_CLIENT_ID", ""), ClientSecret: value("ANNOLUX_GITHUB_CLIENT_SECRET", ""),
			RedirectURL: value("ANNOLUX_GITHUB_REDIRECT_URL", apiPublicURL+"/api/v1/auth/github/callback"),
		},
	}
	// Redirect URLs alone must not make an otherwise unconfigured OAuth block
	// appear enabled.
	if config.OAuth.Google.ClientID == "" && config.OAuth.Google.ClientSecret == "" && config.OAuth.GitHub.ClientID == "" && config.OAuth.GitHub.ClientSecret == "" {
		config.OAuth = publicapi.OAuthConfig{}
	}
	liveEnabled := envBool(lookup, "ANNOLUX_PADDLE_LIVE_ENABLED") && envBool(lookup, "ANNOLUX_PADDLE_LIVE_APPROVED")
	config.Billing = publicapi.BillingConfig{
		Environment: value("ANNOLUX_PADDLE_ENVIRONMENT", "sandbox"),
		APIKey:      value("ANNOLUX_PADDLE_API_KEY", ""), ClientToken: value("ANNOLUX_PADDLE_CLIENT_TOKEN", ""),
		WebhookSecret: value("ANNOLUX_PADDLE_WEBHOOK_SECRET", ""),
		ProPriceID:    value("ANNOLUX_PADDLE_PRO_PRICE_ID", ""), ScalePriceID: value("ANNOLUX_PADDLE_SCALE_PRICE_ID", ""),
		TermsVersion: config.TermsVersion, RefundVersion: config.RefundVersion,
		LiveEnabled: liveEnabled,
	}
	if config.Billing.APIKey == "" && config.Billing.ClientToken == "" && config.Billing.WebhookSecret == "" && config.Billing.ProPriceID == "" && config.Billing.ScalePriceID == "" {
		config.Billing = publicapi.BillingConfig{}
	}
	if host, port, err := net.SplitHostPort(config.ListenAddr); err != nil || host != "127.0.0.1" && host != "0.0.0.0" || port == "" {
		return runtimeConfig{}, errors.New("ANNOLUX_LISTEN_ADDR must use 127.0.0.1 or 0.0.0.0 with a port")
	}
	// Fusion ships dark: absent or false, the search path is bit-for-bit the
	// frozen core query. The knobs exist only for the v3 dev split.
	config.Fusion = searchfuse.Config{Enabled: envBool(lookup, "ANNOLUX_TIER1_FUSION")}
	if raw := value("ANNOLUX_TIER1_RRF_K", ""); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > 1000 {
			return runtimeConfig{}, errors.New("ANNOLUX_TIER1_RRF_K must be between 1 and 1000")
		}
		config.Fusion.RRFK = parsed
	}
	if raw := value("ANNOLUX_TIER1_WINDOW", ""); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 10 || parsed > 200 {
			return runtimeConfig{}, errors.New("ANNOLUX_TIER1_WINDOW must be between 10 and 200")
		}
		config.Fusion.Window = parsed
	}
	return config, nil
}

func envBool(lookup lookupEnv, name string) bool {
	raw, ok := lookup(name)
	if !ok {
		return false
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && value
}
