package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeStore is an in-memory Store. failWith, when set, is returned by every
// call so error paths can be tested.
type fakeStore struct {
	todos    map[int64]Todo
	nextID   int64
	failWith error
}

func newFakeStore() *fakeStore {
	return &fakeStore{todos: map[int64]Todo{}, nextID: 1}
}

func (f *fakeStore) List(context.Context) ([]Todo, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	var out []Todo
	for id := int64(1); id < f.nextID; id++ {
		if t, ok := f.todos[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeStore) Get(_ context.Context, id int64) (Todo, error) {
	if f.failWith != nil {
		return Todo{}, f.failWith
	}
	t, ok := f.todos[id]
	if !ok {
		return Todo{}, ErrNotFound
	}
	return t, nil
}

func (f *fakeStore) Create(_ context.Context, title string) (Todo, error) {
	if f.failWith != nil {
		return Todo{}, f.failWith
	}
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	t := Todo{ID: f.nextID, Title: title, CreatedAt: now, UpdatedAt: now}
	f.todos[t.ID] = t
	f.nextID++
	return t, nil
}

func (f *fakeStore) Update(_ context.Context, id int64, p Patch) (Todo, error) {
	if f.failWith != nil {
		return Todo{}, f.failWith
	}
	t, ok := f.todos[id]
	if !ok {
		return Todo{}, ErrNotFound
	}
	if p.Title != nil {
		t.Title = *p.Title
	}
	if p.Done != nil {
		t.Done = *p.Done
	}
	f.todos[id] = t
	return t, nil
}

func (f *fakeStore) Delete(_ context.Context, id int64) error {
	if f.failWith != nil {
		return f.failWith
	}
	if _, ok := f.todos[id]; !ok {
		return ErrNotFound
	}
	delete(f.todos, id)
	return nil
}

func (f *fakeStore) Ping(context.Context) error { return f.failWith }

func serve(t *testing.T, store Store, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	NewHandler(store, slog.New(slog.DiscardHandler)).ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestCRUD(t *testing.T) {
	store := newFakeStore()

	rec := serve(t, store, http.MethodGet, "/api/todos", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty list = %d %q, want 200 []", rec.Code, rec.Body.String())
	}

	rec = serve(t, store, http.MethodPost, "/api/todos", `{"title":"  write the migration  "}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/api/todos/1" {
		t.Fatalf("Location = %q", loc)
	}
	if created := decodeBody[Todo](t, rec); created.Title != "write the migration" || created.Done {
		t.Fatalf("created = %+v, want trimmed title and not done", created)
	}

	rec = serve(t, store, http.MethodPatch, "/api/todos/1", `{"done":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body.String())
	}
	if updated := decodeBody[Todo](t, rec); !updated.Done || updated.Title != "write the migration" {
		t.Fatalf("updated = %+v", updated)
	}

	rec = serve(t, store, http.MethodGet, "/api/todos/1", "")
	if got := decodeBody[Todo](t, rec); rec.Code != http.StatusOK || !got.Done {
		t.Fatalf("get = %d %+v", rec.Code, got)
	}

	rec = serve(t, store, http.MethodDelete, "/api/todos/1", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	rec = serve(t, store, http.MethodGet, "/api/todos/1", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete = %d, want 404", rec.Code)
	}
}

func TestRejectsBadRequests(t *testing.T) {
	long := strings.Repeat("é", MaxTitleLength+1)
	tests := []struct {
		name, method, path, body string
		want                     int
	}{
		{"empty title", http.MethodPost, "/api/todos", `{"title":"   "}`, http.StatusBadRequest},
		{"missing title", http.MethodPost, "/api/todos", `{}`, http.StatusBadRequest},
		{"title too long", http.MethodPost, "/api/todos", `{"title":"` + long + `"}`, http.StatusBadRequest},
		{"unknown field", http.MethodPost, "/api/todos", `{"title":"a","owner":"b"}`, http.StatusBadRequest},
		{"not JSON", http.MethodPost, "/api/todos", `title=a`, http.StatusBadRequest},
		{"two objects", http.MethodPost, "/api/todos", `{"title":"a"}{"title":"b"}`, http.StatusBadRequest},
		{"body too large", http.MethodPost, "/api/todos", `{"title":"` + strings.Repeat("a", maxBodyBytes) + `"}`, http.StatusRequestEntityTooLarge},
		{"empty patch", http.MethodPatch, "/api/todos/1", `{}`, http.StatusBadRequest},
		{"blank title patch", http.MethodPatch, "/api/todos/1", `{"title":""}`, http.StatusBadRequest},
		{"non-numeric id", http.MethodGet, "/api/todos/abc", "", http.StatusBadRequest},
		{"zero id", http.MethodDelete, "/api/todos/0", "", http.StatusBadRequest},
		{"unknown id", http.MethodPatch, "/api/todos/99", `{"done":true}`, http.StatusNotFound},
		{"unknown id delete", http.MethodDelete, "/api/todos/99", "", http.StatusNotFound},
		{"wrong method", http.MethodPut, "/api/todos/1", `{"title":"a"}`, http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, newFakeStore(), tt.method, tt.path, tt.body)
			if rec.Code != tt.want {
				t.Fatalf("%s %s = %d %s, want %d", tt.method, tt.path, rec.Code, rec.Body.String(), tt.want)
			}
		})
	}
}

func TestTitleLimitCountsCharacters(t *testing.T) {
	// 200 two-byte characters is 400 bytes but within the limit.
	title := strings.Repeat("é", MaxTitleLength)
	rec := serve(t, newFakeStore(), http.MethodPost, "/api/todos", `{"title":"`+title+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s, want 201", rec.Code, rec.Body.String())
	}
}

func TestStoreErrorsAreNotLeaked(t *testing.T) {
	store := newFakeStore()
	store.failWith = errors.New(`pq: permission denied for table todos at host 10.0.0.7`)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/todos", ""},
		{http.MethodPost, "/api/todos", `{"title":"a"}`},
		{http.MethodGet, "/api/todos/1", ""},
	} {
		rec := serve(t, store, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s %s = %d, want 500", tc.method, tc.path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "10.0.0.7") || strings.Contains(rec.Body.String(), "permission") {
			t.Fatalf("%s %s leaked the store error: %s", tc.method, tc.path, rec.Body.String())
		}
	}
}

func TestProbes(t *testing.T) {
	store := newFakeStore()
	if rec := serve(t, store, http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d", rec.Code)
	}
	if rec := serve(t, store, http.MethodGet, "/readyz", ""); rec.Code != http.StatusOK {
		t.Fatalf("readyz with a healthy store = %d", rec.Code)
	}

	store.failWith = errors.New("connection refused")
	if rec := serve(t, store, http.MethodGet, "/readyz", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with a failing store = %d, want 503", rec.Code)
	}
	// Liveness must not depend on the database, or an outage restarts every pod.
	if rec := serve(t, store, http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
		t.Fatalf("healthz with a failing store = %d, want 200", rec.Code)
	}
}
