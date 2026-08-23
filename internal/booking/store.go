package booking

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	ErrConflict       = errors.New("комната уже занята на это время")
	ErrNotFound       = errors.New("брони с таким id нет")
	ErrNotCancelled   = errors.New("бронь не отменена")
	ErrRestoreExpired = errors.New("окно восстановления истекло")
)

type Store struct {
	mu            sync.RWMutex
	rooms         map[string][]Booking
	now           func() time.Time
	restoreWindow time.Duration
}

func NewStore(now func() time.Time, restoreWindow time.Duration) *Store {
	return &Store{rooms: make(map[string][]Booking), now: now, restoreWindow: restoreWindow}
}

func (s *Store) Create(room string, start, end time.Time) (Booking, error) {
	id, err := newID()
	if err != nil {
		return Booking{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, b := range s.rooms[room] {
		if b.CancelledAt != nil {
			continue
		}
		if start.Before(b.End) && b.Start.Before(end) {
			return Booking{}, ErrConflict
		}
	}

	b := Booking{ID: id, Room: room, Start: start, End: end}
	s.rooms[room] = append(s.rooms[room], b)
	return b, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for room, bookings := range s.rooms {
		for i, b := range bookings {
			if b.ID != id || b.CancelledAt != nil {
				continue
			}
			cancelled := s.now()
			b.CancelledAt = &cancelled
			s.rooms[room][i] = b
			return nil
		}
	}
	return ErrNotFound
}

func (s *Store) Restore(id string) (Booking, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, bookings := range s.rooms {
		for i, b := range bookings {
			if b.ID != id {
				continue
			}
			if b.CancelledAt == nil {
				return Booking{}, ErrNotCancelled
			}
			if s.now().Sub(*b.CancelledAt) > s.restoreWindow {
				return Booking{}, ErrRestoreExpired
			}
			for _, other := range bookings {
				if other.CancelledAt == nil && b.Start.Before(other.End) && other.Start.Before(b.End) {
					return Booking{}, ErrConflict
				}
			}
			b.CancelledAt = nil
			bookings[i] = b
			return b, nil
		}
	}
	return Booking{}, ErrNotFound
}

func (s *Store) ListByDate(room string, date time.Time) []Booking {
	dayStart := date.UTC().Truncate(24 * time.Hour)
	dayEnd := dayStart.AddDate(0, 0, 1)

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Booking, 0)
	for _, b := range s.rooms[room] {
		if b.CancelledAt != nil {
			continue
		}
		if !b.Start.Before(dayStart) && b.Start.Before(dayEnd) {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}
