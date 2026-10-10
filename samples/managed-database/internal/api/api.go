// Package api serves the todo HTTP API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Todo is one item on the list.
type Todo struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Patch is a partial update. A nil field is left unchanged.
type Patch struct {
	Title *string `json:"title"`
	Done  *bool   `json:"done"`
}

// ErrNotFound is returned by a Store when no todo has the requested ID.
var ErrNotFound = errors.New("todo not found")

// Store is the persistence the API needs. The application uses PostgreSQL;
// tests use an in-memory fake.
type Store interface {
	List(ctx context.Context) ([]Todo, error)
	Get(ctx context.Context, id int64) (Todo, error)
	Create(ctx context.Context, title string) (Todo, error)
	Update(ctx context.Context, id int64, patch Patch) (Todo, error)
	Delete(ctx context.Context, id int64) error
	Ping(ctx context.Context) error
}

const (
	// MaxTitleLength matches the CHECK constraint on todos.title.
	MaxTitleLength = 200
	maxBodyBytes   = 4 << 10
	pingTimeout    = 2 * time.Second
)

type handler struct {
	store  Store
	logger *slog.Logger
}

// NewHandler returns the API routes. Only /api/* is meant to be exposed
// publicly; /healthz and /readyz are for the kubelet.
func NewHandler(store Store, logger *slog.Logger) http.Handler {
	h := &handler{store: store, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/todos", h.list)
	mux.HandleFunc("POST /api/todos", h.create)
	mux.HandleFunc("GET /api/todos/{id}", h.get)
	mux.HandleFunc("PATCH /api/todos/{id}", h.update)
	mux.HandleFunc("DELETE /api/todos/{id}", h.remove)
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
	return mux
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	todos, err := h.store.List(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if todos == nil {
		todos = []Todo{}
	}
	writeJSON(w, http.StatusOK, todos)
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	todo, err := h.store.Get(r.Context(), id)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, todo)
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
	}
	if !decode(w, r, &body) {
		return
	}
	title, msg := validTitle(body.Title)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	todo, err := h.store.Create(r.Context(), title)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/todos/"+strconv.FormatInt(todo.ID, 10))
	writeJSON(w, http.StatusCreated, todo)
}

func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var patch Patch
	if !decode(w, r, &patch) {
		return
	}
	if patch.Title == nil && patch.Done == nil {
		writeError(w, http.StatusBadRequest, "nothing to update: set title or done")
		return
	}
	if patch.Title != nil {
		title, msg := validTitle(*patch.Title)
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		patch.Title = &title
	}
	todo, err := h.store.Update(r.Context(), id, patch)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, todo)
}

func (h *handler) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		h.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

// readyz reports whether the database answers. A pod that cannot reach it is
// taken out of the Service rather than restarted: liveness uses /healthz.
func (h *handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
	defer cancel()
	if err := h.store.Ping(ctx); err != nil {
		h.logger.WarnContext(r.Context(), "readiness check failed", slog.String("error", err.Error()))
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

func (h *handler) storeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "todo not found")
		return
	}
	h.internalError(w, r, err)
}

// internalError logs the cause and returns a generic message, so database
// errors never reach the browser.
func (h *handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "request failed",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("error", err.Error()),
	)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

// decode reads a single JSON object of bounded size with no unknown fields.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request body must be a single JSON object")
		return false
	}
	return true
}

// validTitle trims the title and returns an error message when it is empty
// or too long.
func validTitle(raw string) (string, string) {
	title := strings.TrimSpace(raw)
	switch {
	case title == "":
		return "", "title is required"
	// Characters, not bytes, to match char_length() in the table's CHECK.
	case utf8.RuneCountInString(title) > MaxTitleLength:
		return "", fmt.Sprintf("title must be at most %d characters", MaxTitleLength)
	}
	return title, ""
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
