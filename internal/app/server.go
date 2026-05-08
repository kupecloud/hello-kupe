package app

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	cfg           Config
	startedAt     time.Time
	template      *template.Template
	logger        *slog.Logger
	mu            sync.Mutex
	requestCounts map[requestMetricKey]uint64
	eventCounts   map[string]uint64
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

type eventMetricEntry struct {
	Level string
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
	tpl, err := newHomeTemplate()
	if err != nil {
		return nil, fmt.Errorf("parse home template: %w", err)
	}

	return &Server{
		cfg:           cfg,
		startedAt:     time.Now().UTC(),
		template:      tpl,
		logger:        slog.New(slog.NewJSONHandler(output, nil)),
		requestCounts: make(map[requestMetricKey]uint64),
		eventCounts:   make(map[string]uint64),
	}, nil
}

func (a *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleHome)
	mux.HandleFunc("/api/hello", a.handleHello)
	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/readyz", handleHealth)
	mux.HandleFunc("/metrics", a.handleMetrics)

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", a.cfg.Port),
		Handler:           a.loggingMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go a.backgroundLogger(ctx)

	a.log(ctx, slog.LevelInfo, "service started",
		slog.String("service", a.cfg.ServiceName),
		slog.String("tenant", a.cfg.Tenant),
		slog.String("public_url", a.cfg.PublicURL),
		slog.String("pod", a.cfg.PodName),
		slog.String("namespace", a.cfg.PodNamespace),
		slog.String("listen_addr", httpServer.Addr),
	)

	go func() {
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
		return fmt.Errorf("listen failed: %w", err)
	}

	return nil
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (a *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.template.Execute(w, a.cfg)
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
		a.recordRequest(r.Method, r.URL.Path, recorder.status)

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
			slog.String("path", r.URL.Path),
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

func (a *Server) backgroundLogger(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.LogInterval)
	defer ticker.Stop()

	levels := []slog.Level{slog.LevelInfo, slog.LevelInfo, slog.LevelInfo, slog.LevelInfo, slog.LevelWarn, slog.LevelError, slog.LevelDebug}
	statuses := []int{200, 200, 200, 201, 301, 400, 404, 500, 503}
	methods := []string{"GET", "GET", "GET", "POST", "PUT", "DELETE"}
	paths := []string{"/api/users", "/api/orders", "/api/products", "/api/health", "/api/auth/login", "/api/settings"}
	users := []string{"user-1", "user-2", "user-3", "admin", "service-account"}

	i := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i++
			level := levels[randomIndex(len(levels))]
			status := statuses[randomIndex(len(statuses))]
			method := methods[randomIndex(len(methods))]
			path := paths[randomIndex(len(paths))]
			duration := randomIndex(500) + 1
			user := users[randomIndex(len(users))]

			a.recordEvent(level.String())
			a.log(ctx, level, fmt.Sprintf("%s %s", method, path),
				slog.String("service", a.cfg.ServiceName),
				slog.String("tenant", a.cfg.Tenant),
				slog.String("request_id", fmt.Sprintf("bg-%d", i)),
				slog.String("method", method),
				slog.String("path", path),
				slog.Int("status", status),
				slog.Int("duration_ms", duration),
				slog.String("user", user),
				slog.String("pod", a.cfg.PodName),
				slog.String("namespace", a.cfg.PodNamespace),
				slog.Bool("synthetic", true),
				slog.String("public_url", a.cfg.PublicURL),
				slog.String("message_kind", "background-demo-log"),
			)
		}
	}
}

func (a *Server) recordRequest(method, path string, status int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	key := requestMetricKey{Method: method, Path: path, Status: status}
	a.requestCounts[key]++
}

func (a *Server) recordEvent(level string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.eventCounts[level]++
}

func (a *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	requestCounts, eventCounts := a.snapshotMetrics()
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

	builder.WriteString("# HELP hello_kupe_background_events_total Total synthetic background events emitted by hello-kupe.\n")
	builder.WriteString("# TYPE hello_kupe_background_events_total counter\n")
	for _, entry := range eventCounts {
		fmt.Fprintf(&builder, "hello_kupe_background_events_total{level=%q} %d\n",
			escapeLabelValue(entry.Level),
			entry.Count)
	}

	_, _ = w.Write([]byte(builder.String()))
}

func (a *Server) snapshotMetrics() ([]requestMetricEntry, []eventMetricEntry) {
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

	events := make([]eventMetricEntry, 0, len(a.eventCounts))
	for level, count := range a.eventCounts {
		events = append(events, eventMetricEntry{Level: level, Count: count})
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Level < events[j].Level
	})

	return requests, events
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

func randomIndex(limit int) int {
	if limit <= 1 {
		return 0
	}

	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0
	}

	return int(n.Int64())
}
