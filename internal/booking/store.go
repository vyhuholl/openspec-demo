package booking

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

var ErrConflict = errors.New("room is already booked for this time")

type Booking struct {
	ID    string    `json:"id"`
	Room  string    `json:"room"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type Store struct {
	mu     sync.RWMutex
	byRoom map[string][]Booking
}

func NewStore() *Store {
	return &Store{byRoom: make(map[string][]Booking)}
}

func (s *Store) Create(room string, start, end time.Time) (Booking, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, b := range s.byRoom[room] {
		if start.Before(b.End) && b.Start.Before(end) {
			return Booking{}, fmt.Errorf("%w: conflicts with booking %s", ErrConflict, b.ID)
		}
	}

	b := Booking{ID: newID(), Room: room, Start: start, End: end}
	s.byRoom[room] = append(s.byRoom[room], b)
	return b, nil
}

func (s *Store) List(room string, from, to time.Time) []Booking {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := []Booking{}
	for _, b := range s.byRoom[room] {
		if b.Start.Before(to) && from.Before(b.End) {
			result = append(result, b)
		}
	}
	slices.SortFunc(result, func(a, b Booking) int { return a.Start.Compare(b.Start) })
	return result
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
