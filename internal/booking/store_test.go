package booking

import (
	"errors"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func mustStoreCreate(t *testing.T, s *Store, room, start, end string) Booking {
	t.Helper()
	b, err := s.Create(room, mustTime(t, start), mustTime(t, end))
	if err != nil {
		t.Fatalf("Create(%q, %s, %s): %v", room, start, end, err)
	}
	return b
}

// storeSize reads the internal map so tests can assert that no booking
// appeared in any room, including rooms a request could never query.
func storeSize(s *Store) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, bs := range s.byRoom {
		n += len(bs)
	}
	return n
}

func mustDay(t *testing.T, date string) (from, to time.Time) {
	t.Helper()
	from, err := time.Parse(time.DateOnly, date)
	if err != nil {
		t.Fatalf("parse %q: %v", date, err)
	}
	return from, from.AddDate(0, 0, 1)
}

func TestStoreCreate_Valid_ReturnsBookingInUTC(t *testing.T) {
	tests := []struct {
		name  string
		start string
		end   string
	}{
		{"суффикс Z", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"},
		{"смещение +00:00", "2027-11-01T09:00:00+00:00", "2027-11-01T10:00:00+00:00"},
		{"смещение -00:00", "2027-11-01T09:00:00-00:00", "2027-11-01T10:00:00-00:00"},
		{"дробные секунды", "2027-11-01T09:00:00.5Z", "2027-11-01T10:00:00.123456789Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()
			start, end := mustTime(t, tt.start), mustTime(t, tt.end)

			b, err := s.Create("green", start, end)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if b.ID == "" {
				t.Fatalf("ID is empty")
			}
			if b.Room != "green" {
				t.Fatalf("Room = %q, want %q", b.Room, "green")
			}
			if !b.Start.Equal(start) || !b.End.Equal(end) {
				t.Fatalf("interval = %s–%s, want %s–%s", b.Start, b.End, start, end)
			}
			if b.Start.Location() != time.UTC || b.End.Location() != time.UTC {
				t.Fatalf("locations = %v, %v, want UTC", b.Start.Location(), b.End.Location())
			}
			if n := storeSize(s); n != 1 {
				t.Fatalf("store size = %d, want 1", n)
			}
		})
	}
}

func TestStoreCreate_Invalid_ReturnsErrInvalid(t *testing.T) {
	tests := []struct {
		name  string
		room  string
		start string
		end   string
	}{
		{"пустая комната", "", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"},
		{"комната из пробелов", "   ", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"},
		{"табуляция и перевод строки", "\t\n", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"},
		{"неразрывный пробел", "\u00a0", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"},
		{"end равен start", "green", "2027-11-01T09:00:00Z", "2027-11-01T09:00:00Z"},
		{"end раньше start на 1 нс", "green", "2027-11-01T09:00:00.000000001Z", "2027-11-01T09:00:00Z"},
		{"end раньше start на час", "green", "2027-11-01T09:00:00Z", "2027-11-01T08:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()
			mustStoreCreate(t, s, "green", "2027-11-01T12:00:00Z", "2027-11-01T13:00:00Z")

			_, err := s.Create(tt.room, mustTime(t, tt.start), mustTime(t, tt.end))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if n := storeSize(s); n != 1 {
				t.Fatalf("store size = %d, want 1", n)
			}
		})
	}
}

func TestStoreCreate_NonBlankRoom_Succeeds(t *testing.T) {
	tests := []struct {
		name string
		room string
	}{
		{"один символ", "g"},
		{"один символ с пробелами по краям", " g "},
		{"zero-width space", "\u200b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()

			b := mustStoreCreate(t, s, tt.room, "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
			if b.Room != tt.room {
				t.Fatalf("Room = %q, want %q", b.Room, tt.room)
			}
		})
	}
}

func TestStoreCreate_Overlap_ReturnsErrConflict(t *testing.T) {
	tests := []struct {
		name  string
		start string
		end   string
	}{
		{"частичное пересечение в конце", "2027-11-01T09:30:00Z", "2027-11-01T10:30:00Z"},
		{"частичное пересечение в начале", "2027-11-01T08:30:00Z", "2027-11-01T09:30:00Z"},
		{"новая вложена в существующую", "2027-11-01T09:15:00Z", "2027-11-01T09:45:00Z"},
		{"новая охватывает существующую", "2027-11-01T08:00:00Z", "2027-11-01T11:00:00Z"},
		{"интервалы совпадают", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"},
		{"пересечение на 1 с у конца", "2027-11-01T09:59:59Z", "2027-11-01T11:00:00Z"},
		{"пересечение на 1 с у начала", "2027-11-01T08:00:00Z", "2027-11-01T09:00:01Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()
			existing := mustStoreCreate(t, s, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")

			_, err := s.Create("green", mustTime(t, tt.start), mustTime(t, tt.end))
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("err = %v, want ErrConflict", err)
			}
			if n := storeSize(s); n != 1 {
				t.Fatalf("store size = %d, want 1", n)
			}
			if got := s.byRoom["green"][0]; got != existing {
				t.Fatalf("existing booking changed: %+v, want %+v", got, existing)
			}
		})
	}
}

func TestStoreCreate_TouchingOrOtherRoom_Succeeds(t *testing.T) {
	tests := []struct {
		name  string
		room  string
		start string
		end   string
	}{
		{"касание после существующей", "green", "2027-11-01T10:00:00Z", "2027-11-01T11:00:00Z"},
		{"касание перед существующей", "green", "2027-11-01T08:00:00Z", "2027-11-01T09:00:00Z"},
		{"другой регистр комнаты", "Green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"},
		{"пробел перед именем комнаты", " green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"},
		{"другая комната", "blue", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()
			mustStoreCreate(t, s, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")

			if _, err := s.Create(tt.room, mustTime(t, tt.start), mustTime(t, tt.end)); err != nil {
				t.Fatalf("Create: %v", err)
			}
			if n := storeSize(s); n != 2 {
				t.Fatalf("store size = %d, want 2", n)
			}
		})
	}
}

func TestStoreListDay_DayBoundaries_MatchesHalfOpenDay(t *testing.T) {
	tests := []struct {
		name    string
		start   string
		end     string
		visible bool
	}{
		{"заканчивается ровно в начале суток", "2027-10-31T22:00:00Z", "2027-11-01T00:00:00Z", false},
		{"заходит в сутки на 1 с", "2027-10-31T22:00:00Z", "2027-11-01T00:00:01Z", true},
		{"начинается ровно в начале суток", "2027-11-01T00:00:00Z", "2027-11-01T01:00:00Z", true},
		{"целиком внутри суток", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z", true},
		{"начинается за 1 с до конца суток", "2027-11-01T23:59:59Z", "2027-11-02T01:00:00Z", true},
		{"начинается ровно в конце суток", "2027-11-02T00:00:00Z", "2027-11-02T01:00:00Z", false},
		{"охватывает сутки целиком", "2027-10-31T20:00:00Z", "2027-11-02T04:00:00Z", true},
		{"в других сутках", "2027-10-30T09:00:00Z", "2027-10-30T10:00:00Z", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()
			b := mustStoreCreate(t, s, "green", tt.start, tt.end)
			from, to := mustDay(t, "2027-11-01")

			got := s.ListDay("green", from, to)
			if tt.visible && (len(got) != 1 || got[0] != b) {
				t.Fatalf("ListDay = %+v, want exactly %+v", got, b)
			}
			if !tt.visible && len(got) != 0 {
				t.Fatalf("ListDay = %+v, want empty", got)
			}
		})
	}
}

func TestStoreListDay_AcrossMidnight_VisibleInBothDays(t *testing.T) {
	s := NewStore()
	b := mustStoreCreate(t, s, "green", "2027-11-01T23:00:00Z", "2027-11-02T01:00:00Z")

	tests := []struct {
		date string
		want int
	}{
		{"2027-10-31", 0},
		{"2027-11-01", 1},
		{"2027-11-02", 1},
		{"2027-11-03", 0},
	}
	for _, tt := range tests {
		t.Run(tt.date, func(t *testing.T) {
			from, to := mustDay(t, tt.date)

			got := s.ListDay("green", from, to)
			if len(got) != tt.want {
				t.Fatalf("len(ListDay) = %d, want %d", len(got), tt.want)
			}
			if tt.want == 1 && got[0] != b {
				t.Fatalf("ListDay[0] = %+v, want %+v", got[0], b)
			}
		})
	}
}

func TestStoreListDay_Unordered_SortedByStart(t *testing.T) {
	s := NewStore()
	mustStoreCreate(t, s, "green", "2027-11-01T15:00:00Z", "2027-11-01T16:00:00Z")
	mustStoreCreate(t, s, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
	mustStoreCreate(t, s, "green", "2027-10-31T23:00:00Z", "2027-11-01T01:00:00Z")
	mustStoreCreate(t, s, "green", "2027-11-01T12:00:00Z", "2027-11-01T12:30:00Z")
	from, to := mustDay(t, "2027-11-01")

	got := s.ListDay("green", from, to)

	want := []string{"2027-10-31T23:00:00Z", "2027-11-01T09:00:00Z", "2027-11-01T12:00:00Z", "2027-11-01T15:00:00Z"}
	if len(got) != len(want) {
		t.Fatalf("len(ListDay) = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if !got[i].Start.Equal(mustTime(t, w)) {
			t.Fatalf("ListDay[%d].Start = %s, want %s", i, formatTime(got[i].Start), w)
		}
	}
}

func TestStoreListDay_OtherRooms_Excluded(t *testing.T) {
	s := NewStore()
	for _, room := range []string{"green", "blue", "Green", "green "} {
		mustStoreCreate(t, s, room, "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
	}
	from, to := mustDay(t, "2027-11-01")

	got := s.ListDay("green", from, to)
	if len(got) != 1 || got[0].Room != "green" {
		t.Fatalf("ListDay = %+v, want exactly one booking of room %q", got, "green")
	}
}

func TestStoreListDay_NoBookings_ReturnsNonNilEmpty(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, s *Store)
	}{
		{"броней нет", func(*testing.T, *Store) {}},
		{"бронь комнаты в другие сутки", func(t *testing.T, s *Store) {
			mustStoreCreate(t, s, "green", "2027-11-02T09:00:00Z", "2027-11-02T10:00:00Z")
		}},
		{"бронь в эти сутки у другой комнаты", func(t *testing.T, s *Store) {
			mustStoreCreate(t, s, "blue", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()
			tt.setup(t, s)
			from, to := mustDay(t, "2027-11-01")

			got := s.ListDay("green", from, to)
			if got == nil || len(got) != 0 {
				t.Fatalf("ListDay = %#v, want non-nil empty slice", got)
			}
		})
	}
}

func TestStoreListDay_ResultMutated_StoreUnchanged(t *testing.T) {
	s := NewStore()
	mustStoreCreate(t, s, "green", "2027-11-01T12:00:00Z", "2027-11-01T13:00:00Z")
	mustStoreCreate(t, s, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
	from, to := mustDay(t, "2027-11-01")
	first := s.ListDay("green", from, to)
	want := append([]Booking(nil), first...)

	first[0].Start = first[0].Start.Add(-time.Hour)
	first[0].Room = "blue"
	first[0], first[1] = first[1], first[0]

	got := s.ListDay("green", from, to)
	if len(got) != len(want) {
		t.Fatalf("len(ListDay) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListDay[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
