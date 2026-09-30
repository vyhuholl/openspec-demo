package booking

import (
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalid  = errors.New("invalid booking")
	ErrConflict = errors.New("booking overlaps an existing booking")
)

type Store struct {
	mu     sync.Mutex
	byRoom map[string][]Booking
}

func NewStore() *Store {
	return &Store{byRoom: make(map[string][]Booking)}
}

func (s *Store) Create(room string, start, end time.Time) (Booking, error) {
	if isBlank(room) {
		return Booking{}, fmt.Errorf("%w: room is empty", ErrInvalid)
	}
	if !end.After(start) {
		return Booking{}, fmt.Errorf("%w: end must be after start", ErrInvalid)
	}
	start, end = start.UTC(), end.UTC()

	// The overlap check and the insert share one critical section: otherwise
	// two concurrent requests could both pass the check.
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.byRoom[room] {
		if overlaps(start, end, e.Start, e.End) {
			return Booking{}, fmt.Errorf("%w: room %q is booked from %s to %s",
				ErrConflict, room, formatTime(e.Start), formatTime(e.End))
		}
	}
	b := Booking{ID: rand.Text(), Room: room, Start: start, End: end}
	s.byRoom[room] = append(s.byRoom[room], b)
	return b, nil
}

// ListDay returns a copy, so callers can neither reorder nor edit stored bookings.
func (s *Store) ListDay(room string, from, to time.Time) []Booking {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]Booking, 0)
	for _, e := range s.byRoom[room] {
		if overlaps(e.Start, e.End, from, to) {
			res = append(res, e)
		}
	}
	slices.SortFunc(res, func(a, b Booking) int { return a.Start.Compare(b.Start) })
	return res
}

func isBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}

// overlaps treats both intervals as half-open, so touching intervals do not overlap.
func overlaps(aStart, aEnd, bStart, bEnd time.Time) bool {
	return aStart.Before(bEnd) && bStart.Before(aEnd)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
