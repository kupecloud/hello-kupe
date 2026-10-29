// Package game owns the matching game's rules and public views. The deck never
// leaves the server; clients submit card positions, not scores or timestamps.
package game

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const Pairs = 6
const MismatchDelay = 900 * time.Millisecond
const Lifetime = 30 * time.Minute

var ErrNotFound = errors.New("game expired or not found; start a new game")
var ErrInvalid = errors.New("invalid move")
var ErrCapacity = errors.New("game room is full; try again shortly")

// Feature is the short lesson unlocked by a matching pair.
type Feature struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Detail      string `json:"detail"`
	Link        string `json:"link"`
}

var Features = []Feature{
	{"domains", "Custom domains", "A proper address for your game.", "When deployed on Kupe, Hello Kupe gets a shareable Kupe address. Connect your own domain to give players a familiar link, such as play.your-domain.com, to this very game.", "https://docs.kupe.cloud/networking/custom-domains/"},
	{"database", "Managed PostgreSQL", "A lasting home for game sessions and scores.", "Connect Kupe managed PostgreSQL to store game sessions and saved scores. App replicas can share the same data, keeping the scoreboard available across app restarts and releases.", "https://docs.kupe.cloud/platform/databases/"},
	{"registry", "Container registry", "One image. The whole game.", "Hello Kupe's Next.js frontend and Go API ship together as one container image. Push your build to a Kupe managed registry, then let your cluster pull it to run this game.", "https://docs.kupe.cloud/"},
	{"scaling", "Autoscaling", "More players. The same game.", "Run more copies of Hello Kupe when you need extra capacity. With PostgreSQL connected, every copy shares the same games and scores. Enable autoscaling to add replicas as CPU demand rises.", "https://github.com/kupecloud/hello-kupe#autoscaling"},
	{"observability", "Logs & metrics", "See how this game is running.", "Starting a game, flipping a card and saving a score all make real API requests. Explore their logs, request counts and response times in Grafana to see how Hello Kupe is running.", "https://docs.kupe.cloud/get-started/observe-your-app/"},
	{"gitops", "GitOps deployments", "Ship the next version of this game.", "Keep Hello Kupe's image version and settings in Git. Argo CD can sync a new release or replica count to your cluster, making updates to this game repeatable. With PostgreSQL connected, saved scores live beyond an app release.", "https://docs.kupe.cloud/applications/gitops-workflows/"},
}

// State is persisted as one row and changed under a store lock.
type State struct {
	Deck        []string  `json:"deck"`
	Matched     []bool    `json:"matched"`
	Selected    []int     `json:"selected"`
	Moves       int       `json:"moves"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	RevealUntil time.Time `json:"reveal_until"`
	DurationMS  int64     `json:"duration_ms"`
	Name        string    `json:"name"`
}

type Card struct {
	Index   int    `json:"index"`
	Feature string `json:"feature,omitempty"`
	Matched bool   `json:"matched"`
}

type View struct {
	ID        string `json:"id"`
	Cards     []Card `json:"cards"`
	Moves     int    `json:"moves"`
	Matches   int    `json:"matches"`
	ElapsedMS int64  `json:"elapsed_ms"`
	Complete  bool   `json:"complete"`
	Saved     bool   `json:"saved"`
	RevealMS  int64  `json:"reveal_ms"`
}

type Score struct {
	Name       string `json:"name"`
	Moves      int    `json:"moves"`
	DurationMS int64  `json:"duration_ms"`
}

func New() (string, State, error) {
	var token [24]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", State{}, err
	}
	deck := make([]string, 0, Pairs*2)
	for _, f := range Features {
		deck = append(deck, f.ID, f.ID)
	}
	for i := len(deck) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", State{}, err
		}
		deck[i], deck[n.Int64()] = deck[n.Int64()], deck[i]
	}
	return hex.EncodeToString(token[:]), State{Deck: deck, Matched: make([]bool, len(deck)), Selected: []int{}}, nil
}

func (s *State) Flip(index int, now time.Time) error {
	if index < 0 || index >= len(s.Deck) || !s.CompletedAt.IsZero() || s.Matched[index] {
		return ErrInvalid
	}
	if len(s.Selected) == 2 {
		s.Selected = []int{}
		s.RevealUntil = time.Time{}
	}
	for _, i := range s.Selected {
		if i == index {
			return ErrInvalid
		}
	}
	if s.StartedAt.IsZero() {
		s.StartedAt = now
	}
	s.Selected = append(s.Selected, index)
	if len(s.Selected) < 2 {
		return nil
	}
	s.Moves++
	first := s.Selected[0]
	if s.Deck[first] != s.Deck[index] {
		s.RevealUntil = now.Add(MismatchDelay)
		return nil
	}
	s.Matched[first], s.Matched[index] = true, true
	s.Selected = []int{}
	for _, matched := range s.Matched {
		if !matched {
			return nil
		}
	}
	s.CompletedAt = now
	s.DurationMS = max(1, now.Sub(s.StartedAt).Milliseconds())
	return nil
}

func (s State) View(id string, now time.Time) View {
	v := View{ID: id, Moves: s.Moves, Complete: !s.CompletedAt.IsZero(), Saved: s.Name != "", Cards: make([]Card, len(s.Deck))}
	for i, f := range s.Deck {
		c := Card{Index: i, Matched: s.Matched[i]}
		if c.Matched {
			c.Feature = f
			v.Matches++
		}
		for _, selected := range s.Selected {
			if selected == i && (len(s.Selected) < 2 || now.Before(s.RevealUntil)) {
				c.Feature = f
			}
		}
		v.Cards[i] = c
	}
	v.Matches /= 2
	if !s.StartedAt.IsZero() {
		v.ElapsedMS = max(0, now.Sub(s.StartedAt).Milliseconds())
	}
	if v.Complete {
		v.ElapsedMS = s.DurationMS
	}
	v.RevealMS = max(0, s.RevealUntil.Sub(now).Milliseconds())
	return v
}

func ValidName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 24 {
		return "", fmt.Errorf("name must be between 1 and 24 characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return "", fmt.Errorf("use a printable display name")
		}
	}
	return name, nil
}
