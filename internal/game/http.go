package game

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

type handler struct {
	store  Store
	logger *slog.Logger
}

func Register(mux *http.ServeMux, store Store, logger *slog.Logger) {
	h := &handler{store, logger}
	mux.HandleFunc("GET /api/features", func(w http.ResponseWriter, _ *http.Request) {
		features := append([]Feature(nil), Features...)
		if !store.Persistent() {
			for i := range features {
				if features[i].ID == "database" {
					features[i].Description = "Give this scoreboard a memory."
					features[i].Detail = "Hello Kupe is currently in practice mode, using temporary memory. Its scoreboard resets when the app restarts. Connect managed PostgreSQL to keep game sessions and saved scores shared across restarts and app replicas."
				}
			}
		}
		respond(w, 200, features)
	})
	mux.HandleFunc("GET /api/leaderboard", h.leaderboard)
	mux.HandleFunc("POST /api/games", h.create)
	mux.HandleFunc("GET /api/games/{id}", h.get)
	mux.HandleFunc("POST /api/games/{id}/flip", h.flip)
	mux.HandleFunc("POST /api/games/{id}/score", h.submit)
}
func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	id, s, err := New()
	if err == nil {
		err = h.store.Create(r.Context(), id, s)
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.logger.InfoContext(r.Context(), "game started")
	respond(w, http.StatusCreated, s.View(id, time.Now()))
}
func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	s, err := h.store.Change(r.Context(), r.PathValue("id"), func(*State) error { return nil })
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond(w, 200, s.View(r.PathValue("id"), time.Now()))
}
func (h *handler) flip(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Index *int `json:"index"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Index == nil {
		respond(w, 400, map[string]string{"error": "choose a card"})
		return
	}
	s, err := h.store.Change(r.Context(), r.PathValue("id"), func(s *State) error { return s.Flip(*body.Index, time.Now()) })
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if !s.CompletedAt.IsZero() {
		h.logger.InfoContext(r.Context(), "game completed", slog.Int("moves", s.Moves), slog.Int64("duration_ms", s.DurationMS))
	}
	respond(w, 200, s.View(r.PathValue("id"), time.Now()))
}
func (h *handler) submit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &body) {
		return
	}
	name, err := ValidName(body.Name)
	if err != nil {
		respond(w, 400, map[string]string{"error": err.Error()})
		return
	}
	s, err := h.store.Change(r.Context(), r.PathValue("id"), func(s *State) error {
		if s.CompletedAt.IsZero() {
			return ErrInvalid
		}
		if s.Name != "" && s.Name != name {
			return ErrInvalid
		}
		s.Name = name
		return nil
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.logger.InfoContext(r.Context(), "score saved", slog.Int("moves", s.Moves), slog.Bool("persistent", h.store.Persistent()))
	respond(w, 200, s.View(r.PathValue("id"), time.Now()))
}
func (h *handler) leaderboard(w http.ResponseWriter, r *http.Request) {
	scores, err := h.store.Scores(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond(w, 200, map[string]any{"scores": scores, "persistent": h.store.Persistent()})
}
func (h *handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	status := 500
	message := "The app couldn't reach its data. Please try again."
	switch {
	case errors.Is(err, ErrNotFound):
		status = 404
		message = err.Error()
	case errors.Is(err, ErrInvalid):
		status = 409
		message = "That move isn't available. Refresh or start a new game."
	case errors.Is(err, ErrCapacity):
		status = 503
		message = err.Error()
	default:
		h.logger.ErrorContext(r.Context(), "game request failed", slog.String("error", err.Error()))
	}
	respond(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		respond(w, 400, map[string]string{"error": "invalid request"})
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		respond(w, 400, map[string]string{"error": "send one JSON object"})
		return false
	}
	return true
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// Guard bounds database waits and rejects cross-origin browser writes. The game
// ID is a random bearer token; never put it into a URL log or a metric label.
func Guard(next http.Handler, developmentOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || (u.Host != r.Host && origin != developmentOrigin) {
					http.Error(w, "cross-origin request refused", http.StatusForbidden)
					return
				}
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
