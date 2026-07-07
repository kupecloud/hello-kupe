package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestMethodLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{http.MethodGet, "GET"},
		{http.MethodPost, "POST"},
		{http.MethodDelete, "DELETE"},
		{http.MethodOptions, "OPTIONS"},
		{"FOOBAR1234", "other"},
		{"", "other"},
		{"get", "other"}, // case-sensitive: net/http normalises common verbs, unknown casing is bounded
	}

	for _, tt := range tests {
		if got := methodLabel(tt.in); got != tt.want {
			t.Errorf("methodLabel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// freePort returns a currently-free TCP port on the loopback interface.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// TestRunGracefulShutdown verifies Run serves traffic, and on context
// cancellation returns nil only after the server has finished shutting down —
// i.e. it awaits the drain rather than exiting while Shutdown is still running.
func TestRunGracefulShutdown(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	cfg := Config{
		ServiceName:  "hello-kupe",
		Tenant:       "test",
		PublicURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		PodName:      "test",
		PodNamespace: "test",
		Port:         port,
		LogInterval:  time.Hour, // keep the synthetic logger quiet during the test
	}

	srv, err := NewServer(cfg, io.Discard)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run(ctx) }()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Wait until the server is accepting requests.
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(base + "/healthz") //nolint:noctx // simple readiness poll
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not become ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Confirm a request succeeds while serving.
	resp, err := http.Get(base + "/api/hello") //nolint:noctx // exercised endpoint
	if err != nil {
		t.Fatalf("request while serving failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 while serving, got %d", resp.StatusCode)
	}

	// Trigger shutdown and assert Run returns cleanly within the drain window.
	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error on graceful shutdown: %v", err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("Run did not return after context cancellation (drain not awaited or hung)")
	}

	// After Run returns, the listener must be closed (Shutdown completed) — a
	// successful dial would mean Run returned before the drain finished.
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("server still accepting connections after Run returned")
	}
}
