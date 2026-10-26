package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kupecloud/hello-kupe/internal/game"
)

type Server struct {
	cfg           Config
	startedAt     time.Time
	games         game.Store
	logger        *slog.Logger
	mu            sync.Mutex
	requestCounts map[requestMetricKey]uint64
}

type requestMetricKey struct {
	Method string
	Path   string
	Status int
}

type requestMetricEntry struct {
	Key   requestMetricKey
	Count uint64
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func Run(ctx context.Context, cfg Config, output io.Writer) error {
	server, err := NewServer(cfg, output)
	if err != nil {
		return err
	}

	return server.Run(ctx)
}

func NewServer(cfg Config, output io.Writer) (*Server, error) {
	games, err := openGameStore(cfg)
	if err != nil {
		return nil, fmt.Errorf("configure database: %w", err)
	}

	return &Server{
		cfg:           cfg,
		startedAt:     time.Now().UTC(),
		games:         games,
		logger:        slog.New(slog.NewJSONHandler(output, nil)),
		requestCounts: make(map[requestMetricKey]uint64),
	}, nil
}

func (a *Server) Run(ctx context.Context) error {
	defer a.games.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleHome)
	mux.HandleFunc("/api/hello", a.handleHello)
	mux.HandleFunc("/api/work", a.handleWork)
	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/readyz", a.handleReady)
	game.Register(mux, a.games, a.logger)
	mux.HandleFunc("/metrics", a.handleMetrics)

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", a.cfg.Port),
		Handler:           a.loggingMiddleware(game.Guard(mux, a.cfg.DevelopmentOrigin)),
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	a.log(ctx, slog.LevelInfo, "service started",
		slog.String("service", a.cfg.ServiceName),
		slog.String("tenant", a.cfg.Tenant),
		slog.String("public_url", a.cfg.PublicURL),
		slog.String("pod", a.cfg.PodName),
		slog.String("namespace", a.cfg.PodNamespace),
		slog.String("listen_addr", httpServer.Addr),
	)

	// done is closed once Shutdown has finished draining. ListenAndServe returns
	// http.ErrServerClosed the instant Shutdown is *called*, not when it
	// completes — so we must wait on done before returning, otherwise the process
	// exits mid-drain and in-flight requests are killed (the 10s grace is then
	// dead code).
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		a.log(shutdownCtx, slog.LevelInfo, "service stopping",
			slog.String("service", a.cfg.ServiceName),
			slog.String("tenant", a.cfg.Tenant),
		)

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			a.log(shutdownCtx, slog.LevelError, "shutdown failed", slog.String("error", err.Error()))
		}
	}()

	err := httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		// A bind/listen failure means Shutdown will never be triggered by a
		// clean stop; return immediately rather than block forever on done.
		return fmt.Errorf("listen failed: %w", err)
	}

	// Clean stop: wait for the drain to finish before returning so the caller
	// (and the process) doesn't exit while requests are still being served.
	<-done
	return nil
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (a *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	root := a.cfg.WebRoot
	if root == "" {
		root = "web/out"
	}
	// Only expose generated frontend files, with no directory listings.
	clean := filepath.Clean("/" + r.URL.Path)
	filename := filepath.Join(root, clean)
	info, err := os.Stat(filename)
	if err == nil && info.IsDir() {
		filename = filepath.Join(filename, "index.html")
		_, err = os.Stat(filename)
	}
	if err != nil {
		if r.URL.Path == "/" {
			http.Error(w, "Build the frontend with make web-build before running Hello Kupe.", http.StatusServiceUnavailable)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(r.URL.Path, "/_next/static/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFile(w, r, filename)
}

func (a *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.games.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	handleHealth(w, r)
}

func (a *Server) handleHello(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"message":      "Hello from Kupe Cloud",
		"service":      a.cfg.ServiceName,
		"tenant":       a.cfg.Tenant,
		"public_url":   a.cfg.PublicURL,
		"pod":          a.cfg.PodName,
		"namespace":    a.cfg.PodNamespace,
		"request_path": r.URL.Path,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	})
}

// workDefaultMS is the CPU cost of a /api/work request that does not ask for
// one; workMaxMS caps what a caller may ask for, so a single request cannot
// occupy a core for an unbounded time.
const (
	workDefaultMS = 50
	workMaxMS     = 1000
)

// handleWork burns CPU for roughly the requested number of milliseconds.
//
// It exists so a quickstart deployment can demonstrate horizontal
// autoscaling: drive this endpoint and the pod's CPU rises until the
// HorizontalPodAutoscaler adds replicas. The other endpoints are a JSON
// marshal each (tens of microseconds), so no achievable request rate moves
// CPU utilisation far enough for an HPA to react.
//
// `?ms=` selects the cost per request, default 50 and capped at workMaxMS.
func (a *Server) handleWork(w http.ResponseWriter, r *http.Request) {
	budgetMS := workDefaultMS
	if raw := r.URL.Query().Get("ms"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "ms must be a non-negative integer",
				"max":   workMaxMS,
			})
			return
		}
		budgetMS = parsed
	}
	capped := budgetMS > workMaxMS
	if capped {
		budgetMS = workMaxMS
	}

	started := time.Now()
	rounds := burnCPU(r.Context(), time.Duration(budgetMS)*time.Millisecond)
	writeJSON(w, http.StatusOK, map[string]any{
		"service":      a.cfg.ServiceName,
		"tenant":       a.cfg.Tenant,
		"pod":          a.cfg.PodName,
		"namespace":    a.cfg.PodNamespace,
		"requested_ms": budgetMS,
		"capped":       capped,
		"elapsed_ms":   time.Since(started).Milliseconds(),
		"rounds":       rounds,
	})
}

// burnCPU hashes until the budget is spent and returns the rounds completed.
//
// Real work, not a sleep: a sleeping goroutine consumes no CPU, so an HPA
// scaling on CPU utilisation would never see it. Each digest is fed back into
// the next round so the compiler cannot elide the loop, and the clock is read
// once per checkEvery rounds because time.Now() would otherwise dominate it.
func burnCPU(ctx context.Context, budget time.Duration) uint64 {
	if budget <= 0 {
		return 0
	}
	const checkEvery = 64
	deadline := time.Now().Add(budget)
	buf := make([]byte, sha256.Size)
	var rounds uint64
	for {
		for i := 0; i < checkEvery; i++ {
			sum := sha256.Sum256(buf)
			copy(buf, sum[:])
		}
		rounds += checkEvery
		if time.Now().After(deadline) || ctx.Err() != nil {
			return rounds
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func (a *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		duration := time.Since(started)
		a.recordRequest(methodLabel(r.Method), routeLabel(r.URL.Path), recorder.status)

		if r.URL.Path == "/metrics" || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			return
		}

		level := slog.LevelInfo
		if recorder.status >= 500 {
			level = slog.LevelError
		} else if recorder.status >= 400 {
			level = slog.LevelWarn
		}

		a.log(r.Context(), level, "request served",
			slog.String("service", a.cfg.ServiceName),
			slog.String("tenant", a.cfg.Tenant),
			slog.String("request_id", fmt.Sprintf("req-%d", started.UnixNano())),
			slog.String("method", r.Method),
			slog.String("path", routeLabel(r.URL.Path)),
			slog.Int("status", recorder.status),
			slog.Int64("duration_ms", duration.Milliseconds()),
			slog.String("remote_addr", r.RemoteAddr),
			slog.String("user_agent", r.UserAgent()),
			slog.String("host", r.Host),
			slog.String("pod", a.cfg.PodName),
			slog.String("namespace", a.cfg.PodNamespace),
			slog.String("http_protocol", r.Proto),
		)
	})
}

// knownRoutes is the fixed set of routes registered on the mux. The metric
// path label is bounded to this set so that arbitrary client-supplied paths
// (e.g. scanner traffic hitting "/wp-login.php") cannot mint new label sets.
// Unbounded label cardinality leaks pod memory and explodes time series in the
// shared metrics backend — bound the label to a known route template instead.
var knownRoutes = map[string]struct{}{
	"/":                {},
	"/api/hello":       {},
	"/api/work":        {},
	"/api/games":       {},
	"/api/features":    {},
	"/api/leaderboard": {},
	"/healthz":         {},
	"/readyz":          {},
	"/metrics":         {},
}

// routeLabel maps a raw request path to a bounded metric label. Known routes
// keep their path; everything else collapses into a single "other" bucket.
func routeLabel(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "api" && parts[1] == "games" {
		if len(parts) == 3 {
			return "/api/games/{id}"
		}
		if len(parts) == 4 && (parts[3] == "flip" || parts[3] == "score") {
			return "/api/games/{id}/" + parts[3]
		}
	}

	if _, ok := knownRoutes[path]; ok {
		return path
	}
	return "other"
}

// knownMethods is the fixed set of HTTP verbs the metric method label is bounded
// to. net/http accepts any RFC 7230 token as a method (e.g. `curl -X FOOBAR`),
// so the raw method — like the path — must be collapsed to a bounded label or a
// client looping over random methods mints unbounded time series and leaks pod
// memory (same cardinality class as the path label).
var knownMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodHead:    {},
	http.MethodPost:    {},
	http.MethodPut:     {},
	http.MethodPatch:   {},
	http.MethodDelete:  {},
	http.MethodConnect: {},
	http.MethodOptions: {},
	http.MethodTrace:   {},
}

// methodLabel maps a raw request method to a bounded metric label. Known verbs
// keep their name; everything else collapses into a single "other" bucket.
func methodLabel(method string) string {
	if _, ok := knownMethods[method]; ok {
		return method
	}
	return "other"
}

func (a *Server) recordRequest(method, path string, status int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := requestMetricKey{Method: method, Path: path, Status: status}
	a.requestCounts[key]++
}

func (a *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	requestCounts := a.snapshotMetrics()
	uptime := time.Since(a.startedAt).Seconds()

	var builder strings.Builder
	builder.WriteString("# HELP hello_kupe_build_info Static information about the running hello-kupe instance.\n")
	builder.WriteString("# TYPE hello_kupe_build_info gauge\n")
	fmt.Fprintf(&builder, "hello_kupe_build_info{service=%q,tenant=%q,namespace=%q,pod=%q} 1\n",
		escapeLabelValue(a.cfg.ServiceName),
		escapeLabelValue(a.cfg.Tenant),
		escapeLabelValue(a.cfg.PodNamespace),
		escapeLabelValue(a.cfg.PodName))

	builder.WriteString("# HELP hello_kupe_uptime_seconds Uptime of the hello-kupe process in seconds.\n")
	builder.WriteString("# TYPE hello_kupe_uptime_seconds gauge\n")
	fmt.Fprintf(&builder, "hello_kupe_uptime_seconds %.0f\n", uptime)

	builder.WriteString("# HELP hello_kupe_http_requests_total Total HTTP requests handled by hello-kupe.\n")
	builder.WriteString("# TYPE hello_kupe_http_requests_total counter\n")
	for _, entry := range requestCounts {
		fmt.Fprintf(&builder, "hello_kupe_http_requests_total{method=%q,path=%q,status=%q} %d\n",
			escapeLabelValue(entry.Key.Method),
			escapeLabelValue(entry.Key.Path),
			escapeLabelValue(strconv.Itoa(entry.Key.Status)),
			entry.Count)
	}

	_, _ = w.Write([]byte(builder.String()))
}

func (a *Server) snapshotMetrics() []requestMetricEntry {
	a.mu.Lock()
	defer a.mu.Unlock()

	requests := make([]requestMetricEntry, 0, len(a.requestCounts))
	for key, count := range a.requestCounts {
		requests = append(requests, requestMetricEntry{Key: key, Count: count})
	}
	sort.Slice(requests, func(i, j int) bool {
		if requests[i].Key.Path != requests[j].Key.Path {
			return requests[i].Key.Path < requests[j].Key.Path
		}
		if requests[i].Key.Method != requests[j].Key.Method {
			return requests[i].Key.Method < requests[j].Key.Method
		}
		return requests[i].Key.Status < requests[j].Key.Status
	})

	return requests
}

func (a *Server) log(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) {
	a.logger.LogAttrs(ctx, level, message, attrs...)
}

func escapeLabelValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}
