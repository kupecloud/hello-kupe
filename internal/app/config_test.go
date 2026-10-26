package app

import "testing"

func TestParsePortEnv(t *testing.T) {
	t.Run("returns fallback when unset", func(t *testing.T) {
		t.Setenv("PORT", "")
		if got := parsePortEnv(); got != 8080 {
			t.Fatalf("expected fallback 8080, got %d", got)
		}
	})

	t.Run("returns parsed value", func(t *testing.T) {
		t.Setenv("PORT", "9090")
		if got := parsePortEnv(); got != 9090 {
			t.Fatalf("expected 9090, got %d", got)
		}
	})

	t.Run("returns fallback for invalid values", func(t *testing.T) {
		t.Setenv("PORT", "nope")
		if got := parsePortEnv(); got != 8080 {
			t.Fatalf("expected fallback 8080, got %d", got)
		}
	})
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("SERVICE_NAME", "hello-kupe")
	t.Setenv("TENANT", "acme")
	t.Setenv("PUBLIC_URL", "https://hello-kupe.prod.acme.clusters.kupe.cloud")
	t.Setenv("POD_NAME", "hello-kupe-123")
	t.Setenv("POD_NAMESPACE", "hello")
	t.Setenv("PORT", "8081")

	cfg := LoadConfigFromEnv()

	if cfg.ServiceName != "hello-kupe" {
		t.Fatalf("expected service name hello-kupe, got %q", cfg.ServiceName)
	}
	if cfg.Tenant != "acme" {
		t.Fatalf("expected tenant acme, got %q", cfg.Tenant)
	}
	if cfg.PublicURL != "https://hello-kupe.prod.acme.clusters.kupe.cloud" {
		t.Fatalf("unexpected public url %q", cfg.PublicURL)
	}
	if cfg.Port != 8081 {
		t.Fatalf("expected port 8081, got %d", cfg.Port)
	}
}
