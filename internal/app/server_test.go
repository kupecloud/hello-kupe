package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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

// burnCPU is what makes a CPU-based HPA usable on this app, so its contract
// matters: it must spend roughly the budget, do work the compiler cannot
// elide, and stop early when the caller goes away.
func TestBurnCPU(t *testing.T) {
	t.Parallel()

	if rounds := burnCPU(context.Background(), 0); rounds != 0 {
		t.Errorf("burnCPU(0) did %d rounds, want 0", rounds)
	}

	start := time.Now()
	rounds := burnCPU(context.Background(), 50*time.Millisecond)
	elapsed := time.Since(start)
	if rounds == 0 {
		t.Error("burnCPU(50ms) did no rounds")
	}
	// Generous bounds: a loaded CI runner is slow, but it must neither return
	// instantly (a sleep or an elided loop) nor overshoot wildly.
	if elapsed < 40*time.Millisecond || elapsed > 2*time.Second {
		t.Errorf("burnCPU(50ms) took %s, want roughly 50ms", elapsed)
	}

	// A cancelled request stops the work at the next check rather than
	// burning the whole budget on a client that has gone.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start = time.Now()
	burnCPU(ctx, 5*time.Second)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("burnCPU ignored a cancelled context for %s", elapsed)
	}
}

func TestHandleWork(t *testing.T) {
	t.Parallel()

	srv := &Server{cfg: Config{ServiceName: "hello-kupe", Tenant: "t1", PodName: "p1", PodNamespace: "ns1"}}

	get := func(t *testing.T, query string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/work"+query, nil)
		rec := httptest.NewRecorder()
		srv.handleWork(rec, req)
		var body map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode %q: %v", query, err)
		}
		return rec.Code, body
	}

	// Default cost, and the response identifies the pod that served it —
	// that is how a scale-out test sees the load spread across replicas.
	status, body := get(t, "")
	if status != http.StatusOK {
		t.Fatalf("GET /api/work = %d, want 200", status)
	}
	if body["requested_ms"] != float64(workDefaultMS) {
		t.Errorf("requested_ms = %v, want %d", body["requested_ms"], workDefaultMS)
	}
	if body["pod"] != "p1" || body["tenant"] != "t1" {
		t.Errorf("response does not identify the pod/tenant: %v", body)
	}
	if rounds, _ := body["rounds"].(float64); rounds == 0 {
		t.Error("rounds = 0, so no work was done")
	}

	// Over the cap is clamped, not refused: a load generator asking for too
	// much should still get a bounded amount of work.
	status, body = get(t, fmt.Sprintf("?ms=%d", workMaxMS*10))
	if status != http.StatusOK || body["capped"] != true || body["requested_ms"] != float64(workMaxMS) {
		t.Errorf("over-cap request = %d %v, want 200 capped at %d", status, body, workMaxMS)
	}

	// Zero is legal and free, so a caller can measure the endpoint's overhead.
	if status, body = get(t, "?ms=0"); status != http.StatusOK || body["rounds"] != float64(0) {
		t.Errorf("ms=0 = %d %v, want 200 with no rounds", status, body)
	}

	for _, bad := range []string{"?ms=-1", "?ms=abc", "?ms=1.5"} {
		if status, _ = get(t, bad); status != http.StatusBadRequest {
			t.Errorf("GET /api/work%s = %d, want 400", bad, status)
		}
	}
}
