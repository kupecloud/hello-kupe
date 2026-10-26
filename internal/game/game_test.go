package game

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func knownGame() State {
	return State{Deck: []string{"domains", "database", "domains", "database"}, Matched: make([]bool, 4), Selected: []int{}}
}
func TestRulesAndPrivateDeck(t *testing.T) {
	s := knownGame()
	now := time.Now()
	if v := s.View("secret", now); v.Cards[0].Feature != "" || v.ElapsedMS != 0 {
		t.Fatal("new game leaks a card or starts clock early")
	}
	if err := s.Flip(-1, now); !errors.Is(err, ErrInvalid) {
		t.Fatal("negative index accepted")
	}
	if err := s.Flip(0, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Flip(0, now); !errors.Is(err, ErrInvalid) {
		t.Fatal("same card twice accepted")
	}
	if err := s.Flip(1, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if s.Moves != 1 {
		t.Fatal("a pair of flips is one move")
	}
	if v := s.View("secret", now.Add(2*time.Second)); v.Cards[0].Feature != "" {
		t.Fatal("expired mismatch remains revealed")
	}
	if err := s.Flip(0, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.Flip(2, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.Flip(0, now.Add(4*time.Second)); !errors.Is(err, ErrInvalid) {
		t.Fatal("matched card accepted")
	}
	_ = s.Flip(1, now.Add(4*time.Second))
	_ = s.Flip(3, now.Add(5*time.Second))
	v := s.View("secret", now.Add(time.Hour))
	if !v.Complete || v.Moves != 3 || v.ElapsedMS != 5000 || v.Matches != 2 {
		t.Fatalf("bad completed game: %+v", v)
	}
	if err := s.Flip(1, now.Add(6*time.Second)); !errors.Is(err, ErrInvalid) {
		t.Fatal("completed game changed")
	}
}
func TestNextClickDismissesMismatchAndRevealsChosenCard(t *testing.T) {
	for _, index := range []int{0, 1, 2, 3} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			s := knownGame()
			now := time.Now()
			if err := s.Flip(0, now); err != nil {
				t.Fatal(err)
			}
			if err := s.Flip(1, now); err != nil {
				t.Fatal(err)
			}
			// A third click immediately starts the next turn, even on an old card.
			if err := s.Flip(index, now.Add(time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			v := s.View("test", now.Add(time.Millisecond))
			if v.RevealMS != 0 || v.Moves != 1 || v.Matches != 0 || len(s.Selected) != 1 || s.Selected[0] != index {
				t.Fatalf("third click did not start a new turn: %+v", v)
			}
			for _, card := range v.Cards {
				if (card.Feature != "") != (card.Index == index) {
					t.Fatalf("only the chosen card should remain visible: %+v", v.Cards)
				}
			}
			if !s.StartedAt.Equal(now) {
				t.Fatal("timer restarted after a mismatch")
			}
		})
	}
}
func TestNewGame(t *testing.T) {
	id, s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 48 || len(s.Deck) != 12 {
		t.Fatal("invalid game dimensions or token")
	}
	counts := map[string]int{}
	for _, f := range s.Deck {
		counts[f]++
	}
	if len(counts) != Pairs {
		t.Fatal("missing capabilities")
	}
	for _, count := range counts {
		if count != 2 {
			t.Fatal("each capability must appear twice")
		}
	}
}

type featureTestStore struct {
	Store
	persistent bool
}

func (s featureTestStore) Persistent() bool { return s.persistent }

func TestFeatureHintReflectsActualStorage(t *testing.T) {
	// Alternate modes to catch accidental changes to shared feature definitions.
	for _, persistent := range []bool{false, true, false, true} {
		mux := http.NewServeMux()
		Register(mux, featureTestStore{NewMemory(), persistent}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("GET", "/api/features", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("features returned %d", response.Code)
		}
		var features []Feature
		if err := json.Unmarshal(response.Body.Bytes(), &features); err != nil {
			t.Fatal(err)
		}
		if len(features) != Pairs {
			t.Fatal("missing feature hints")
		}
		found := false
		for _, feature := range features {
			if feature.ID == "database" {
				found = true
				if strings.Contains(feature.Detail, "practice mode") == persistent {
					t.Fatalf("database hint does not reflect persistence=%v: %s", persistent, feature.Detail)
				}
			}
		}
		if !found {
			t.Fatal("database hint missing")
		}
	}
}
func TestMemoryConcurrentMovesAndRollback(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	if err := m.Create(ctx, "test", knownGame()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() { _, _ = m.Change(ctx, "test", func(s *State) error { return s.Flip(0, time.Now()) }) })
	}
	wg.Wait()
	s, err := m.Change(ctx, "test", func(s *State) error { s.Name = "must roll back"; return ErrInvalid })
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid mutation committed")
	}
	s, err = m.Change(ctx, "test", func(*State) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Selected) != 1 || s.Moves != 0 || s.Name != "" {
		t.Fatal("concurrent reveals changed state twice or rollback failed")
	}
}
func TestHTTPScoreValidation(t *testing.T) {
	store := NewMemory()
	mux := http.NewServeMux()
	Register(mux, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		Guard(mux, "").ServeHTTP(w, r)
		return w
	}
	created := call("POST", "/api/games", "{}")
	var v View
	if err := json.Unmarshal(created.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	if strings.Contains(created.Body.String(), "deck") {
		t.Fatal("private deck leaked")
	}
	base := "/api/games/" + v.ID
	for _, body := range []string{`{}`, `{"index":0,"score":999}`, `{"index":0} {"index":1}`, `{"index":999}`, `{"index":null}`} {
		response := call("POST", base+"/flip", body)
		if response.Code < 400 {
			t.Fatalf("accepted invalid body %s", body)
		}
	}
	if w := call("POST", base+"/score", `{"name":"Player"}`); w.Code != 409 {
		t.Fatal("unfinished score accepted")
	}
	s, err := store.Change(context.Background(), v.ID, func(s *State) error {
		s.StartedAt = time.Now().Add(-time.Minute)
		for i := range s.Deck {
			s.Matched[i] = true
		}
		s.Moves = 8
		s.CompletedAt = time.Now()
		s.DurationMS = 60000
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s
	for _, body := range []string{`{"name":""}`, `{"name":"\n"}`, `{"name":"1234567890123456789012345"}`} {
		if w := call("POST", base+"/score", body); w.Code != 400 {
			t.Fatal("invalid name accepted")
		}
	}
	for i := 0; i < 2; i++ {
		if w := call("POST", base+"/score", `{"name":"Player"}`); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	if w := call("POST", base+"/score", `{"name":"Someone else"}`); w.Code != 409 {
		t.Fatal("score overwritten")
	}
	scores, err := store.Scores(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 1 || scores[0].Moves != 8 {
		t.Fatal("score was duplicated or client altered score")
	}
	if w := call("GET", "/api/games/missing", ""); w.Code != 404 {
		t.Fatal("missing game should be 404")
	}
	r := httptest.NewRequest("POST", "/api/games", nil)
	r.Header.Set("Origin", "https://other.example")
	w := httptest.NewRecorder()
	Guard(mux, "").ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross origin write accepted")
	}
}
