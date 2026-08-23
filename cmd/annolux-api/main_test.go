package main

import (
	"encoding/base64"
	"testing"
)

func TestLoadRuntimeConfigUsesOnlyAnnoluxEnvironment(t *testing.T) {
	values := map[string]string{
		"ANNOLUX_ACCOUNT_HMAC_SECRET": base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")),
		"ANNOLUX_INDEX_PATH":          "/tmp/index.db", "ANNOLUX_ACCOUNTS_PATH": "/tmp/accounts.db",
		"ANNOLUX_GOOGLE_CLIENT_ID": "google", "ANNOLUX_GOOGLE_CLIENT_SECRET": "secret",
		"ANNOLUX_GITHUB_CLIENT_ID": "github", "ANNOLUX_GITHUB_CLIENT_SECRET": "secret",
		"ANNOLUX_PADDLE_ENVIRONMENT": "live", "ANNOLUX_PADDLE_LIVE_ENABLED": "true",
		"ANNOLUX_PADDLE_LIVE_APPROVED":  "false",
		"ANNOLUX_MAX_CONCURRENT_SEARCH": "24",
		"ANNOLUX_DEMO_DAILY_BUDGET":     "20000",
	}
	config, err := loadRuntimeConfig(func(name string) (string, bool) { value, ok := values[name]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	if config.ListenAddr != "127.0.0.1:8083" || config.Origin != "https://annolux.com" || config.OAuth.Google.ClientID != "google" || config.OAuth.GitHub.ClientID != "github" || config.Billing.LiveEnabled || config.MaxConcurrent != 24 || config.DemoDailyBudget != 20000 {
		t.Fatalf("config = %#v", config)
	}
}

func TestLoadRuntimeConfigRejectsWeakSecretAndBroadListen(t *testing.T) {
	for _, values := range []map[string]string{
		{"ANNOLUX_ACCOUNT_HMAC_SECRET": "weak"},
		{"ANNOLUX_ACCOUNT_HMAC_SECRET": base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), "ANNOLUX_LISTEN_ADDR": ":8080"},
		{"ANNOLUX_ACCOUNT_HMAC_SECRET": base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), "ANNOLUX_MAX_CONCURRENT_SEARCH": "65"},
		{"ANNOLUX_ACCOUNT_HMAC_SECRET": base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), "ANNOLUX_DEMO_DAILY_BUDGET": "0"},
		{"ANNOLUX_ACCOUNT_HMAC_SECRET": base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")), "ANNOLUX_DEMO_DAILY_BUDGET": "1000001"},
	} {
		if _, err := loadRuntimeConfig(func(name string) (string, bool) { value, ok := values[name]; return value, ok }); err == nil {
			t.Fatalf("accepted invalid values: %#v", values)
		}
	}
}
