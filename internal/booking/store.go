package booking

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	ErrConflict      = errors.New("комната уже занята на это время")
	ErrNotFound      = errors.New("брони с таким id нет")
	ErrRestoreWindow = errors.New("окно восстановления истекло")
)

type cancelled struct {
	b           Booking
	cancelledAt time.Time
}

type Store struct {
	mu            sync.RWMutex
	rooms         map[string][]Booking
	cancelled     map[string]cancelled
	restoreWindow time.Duration
	now           func() time.Time
}

func NewStore(restoreWindow time.Duration) *Store {
	return &Store{
		rooms:         make(map[string][]Booking),
		cancelled:     make(map[string]cancelled),
		restoreWindow: restoreWindow,
		now:           time.Now,
	}
}

func (s *Store) Create(room string, start, end time.Time) (Booking, error) {
	id, err := newID()
	if err != nil {
		return Booking{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conflict(room, start, end) {
		return Booking{}, ErrConflict
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
			if b.ID != id {
				continue
			}
			s.rooms[room] = append(bookings[:i], bookings[i+1:]...)
			if len(s.rooms[room]) == 0 {
				delete(s.rooms, room)
			}
			s.cancelled[id] = cancelled{b: b, cancelledAt: s.now()}
			return nil
		}
	}
	return ErrNotFound
}

func (s *Store) Restore(id string) (Booking, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.cancelled[id]
	if !ok {
		return Booking{}, ErrNotFound
	}
	if !s.now().Before(c.cancelledAt.Add(s.restoreWindow)) {
		return Booking{}, ErrRestoreWindow
	}
	if s.conflict(c.b.Room, c.b.Start, c.b.End) {
		return Booking{}, ErrConflict
	}

	delete(s.cancelled, id)
	s.rooms[c.b.Room] = append(s.rooms[c.b.Room], c.b)
	return c.b, nil
}

// conflict вызывается под блокировкой s.mu.
func (s *Store) conflict(room string, start, end time.Time) bool {
	for _, b := range s.rooms[room] {
		if start.Before(b.End) && b.Start.Before(end) {
			return true
		}
	}
	return false
}

func (s *Store) ListByDate(room string, date time.Time) []Booking {
	dayStart := date.UTC().Truncate(24 * time.Hour)
	dayEnd := dayStart.AddDate(0, 0, 1)

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Booking, 0)
	for _, b := range s.rooms[room] {
		if !b.Start.Before(dayStart) && b.Start.Before(dayEnd) {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}
