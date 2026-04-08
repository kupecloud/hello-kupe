package app

import "testing"

func TestParsePositiveIntEnv(t *testing.T) {
	t.Run("returns fallback when unset", func(t *testing.T) {
		t.Setenv("TEST_PORT", "")
		if got := parsePositiveIntEnv("TEST_PORT", 8080); got != 8080 {
			t.Fatalf("expected fallback 8080, got %d", got)
		}
	})

	t.Run("returns parsed value", func(t *testing.T) {
		t.Setenv("TEST_PORT", "9090")
		if got := parsePositiveIntEnv("TEST_PORT", 8080); got != 9090 {
			t.Fatalf("expected 9090, got %d", got)
		}
	})

	t.Run("returns fallback for invalid values", func(t *testing.T) {
		t.Setenv("TEST_PORT", "nope")
		if got := parsePositiveIntEnv("TEST_PORT", 8080); got != 8080 {
			t.Fatalf("expected fallback 8080, got %d", got)
		}
	})
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("SERVICE_NAME", "hello-kupe")
	t.Setenv("TENANT", "acme")
	t.Setenv("PUBLIC_URL", "https://hello-kupe.acme.kupe.cloud")
	t.Setenv("POD_NAME", "hello-kupe-123")
	t.Setenv("POD_NAMESPACE", "hello")
	t.Setenv("PORT", "8081")
	t.Setenv("LOG_INTERVAL_SECONDS", "9")

	cfg := LoadConfigFromEnv()

	if cfg.ServiceName != "hello-kupe" {
		t.Fatalf("expected service name hello-kupe, got %q", cfg.ServiceName)
	}
	if cfg.Tenant != "acme" {
		t.Fatalf("expected tenant acme, got %q", cfg.Tenant)
	}
	if cfg.PublicURL != "https://hello-kupe.acme.kupe.cloud" {
		t.Fatalf("unexpected public url %q", cfg.PublicURL)
	}
	if cfg.Port != 8081 {
		t.Fatalf("expected port 8081, got %d", cfg.Port)
	}
	if cfg.LogInterval.String() != "9s" {
		t.Fatalf("expected log interval 9s, got %s", cfg.LogInterval)
	}
}
