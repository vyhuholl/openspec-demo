package booking

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type respBooking struct {
	ID    string
	Room  string
	Start string
	End   string
}

func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, r))
	return rec
}

func listTarget(room, date string) string {
	return "/bookings?" + url.Values{"room": {room}, "date": {date}}.Encode()
}

func assertJSONContentType(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, status, rec.Body)
	}
	assertJSONContentType(t, rec)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not a JSON object: %v; body: %s", err, rec.Body)
	}
	if msg, ok := body["error"].(string); !ok || msg == "" {
		t.Fatalf("error field = %#v, want non-empty string; body: %s", body["error"], rec.Body)
	}
}

// decodeBooking also enforces the response contract: exactly id, room, start, end, all strings.
func decodeBooking(t *testing.T, raw []byte) respBooking {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("booking is not a JSON object: %v; raw: %s", err, raw)
	}
	if len(fields) != 4 {
		t.Fatalf("booking has %d fields, want exactly id, room, start, end; raw: %s", len(fields), raw)
	}
	var b respBooking
	for key, dst := range map[string]*string{"id": &b.ID, "room": &b.Room, "start": &b.Start, "end": &b.End} {
		v, ok := fields[key]
		if !ok {
			t.Fatalf("booking has no field %q; raw: %s", key, raw)
		}
		if err := json.Unmarshal(v, dst); err != nil {
			t.Fatalf("booking field %q is not a string: %v; raw: %s", key, err, raw)
		}
	}
	return b
}

func listDay(t *testing.T, h http.Handler, room, date string) []respBooking {
	t.Helper()
	rec := do(t, h, http.MethodGet, listTarget(room, date), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200; body: %s", listTarget(room, date), rec.Code, rec.Body)
	}
	assertJSONContentType(t, rec)
	var body struct {
		Bookings []json.RawMessage `json:"bookings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("list body: %v; body: %s", err, rec.Body)
	}
	res := make([]respBooking, 0, len(body.Bookings))
	for _, raw := range body.Bookings {
		res = append(res, decodeBooking(t, raw))
	}
	return res
}

func createBody(room, start, end string) string {
	b, _ := json.Marshal(map[string]string{"room": room, "start": start, "end": end})
	return string(b)
}

func mustCreate(t *testing.T, h http.Handler, room, start, end string) respBooking {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/bookings", createBody(room, start, end))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %q %s–%s: status = %d, want 201; body: %s", room, start, end, rec.Code, rec.Body)
	}
	assertJSONContentType(t, rec)
	return decodeBooking(t, rec.Body.Bytes())
}

// assertNothingCreated checks the invariant after a rejected request: the store
// still holds only the pre-existing booking, unchanged.
func assertNothingCreated(t *testing.T, s *Store, h http.Handler, date string, existing respBooking) {
	t.Helper()
	if n := storeSize(s); n != 1 {
		t.Fatalf("store size = %d, want 1", n)
	}
	assertOnlyBooking(t, listDay(t, h, existing.Room, date), existing)
}

func assertOnlyBooking(t *testing.T, got []respBooking, want respBooking) {
	t.Helper()
	if len(got) != 1 || got[0] != want {
		t.Fatalf("bookings = %+v, want exactly %+v", got, want)
	}
}

func TestListBookings_DayBoundaries_MatchesHalfOpenDay(t *testing.T) {
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

			got := listDay(t, NewHandler(s), "green", "2027-11-01")
			if tt.visible {
				assertOnlyBooking(t, got, respBooking{b.ID, "green", tt.start, tt.end})
			} else if len(got) != 0 {
				t.Fatalf("bookings = %+v, want empty", got)
			}
		})
	}
}

func TestListBookings_AcrossMidnight_VisibleInBothDays(t *testing.T) {
	s := NewStore()
	b := mustStoreCreate(t, s, "green", "2027-11-01T23:00:00Z", "2027-11-02T01:00:00Z")
	h := NewHandler(s)
	want := respBooking{b.ID, "green", "2027-11-01T23:00:00Z", "2027-11-02T01:00:00Z"}

	tests := []struct {
		date    string
		visible bool
	}{
		{"2027-10-31", false},
		{"2027-11-01", true},
		{"2027-11-02", true},
		{"2027-11-03", false},
	}
	for _, tt := range tests {
		t.Run(tt.date, func(t *testing.T) {
			got := listDay(t, h, "green", tt.date)
			if tt.visible {
				assertOnlyBooking(t, got, want)
			} else if len(got) != 0 {
				t.Fatalf("bookings = %+v, want empty", got)
			}
		})
	}
}

func TestListBookings_EndsAtMidnight_NotInNextDay(t *testing.T) {
	s := NewStore()
	b := mustStoreCreate(t, s, "green", "2027-10-31T22:00:00Z", "2027-11-01T00:00:00Z")
	h := NewHandler(s)

	assertOnlyBooking(t, listDay(t, h, "green", "2027-10-31"),
		respBooking{b.ID, "green", "2027-10-31T22:00:00Z", "2027-11-01T00:00:00Z"})
	if got := listDay(t, h, "green", "2027-11-01"); len(got) != 0 {
		t.Fatalf("bookings for 2027-11-01 = %+v, want empty", got)
	}
}

func TestListBookings_Unordered_SortedByStart(t *testing.T) {
	s := NewStore()
	mustStoreCreate(t, s, "green", "2027-11-01T15:00:00Z", "2027-11-01T16:00:00Z")
	mustStoreCreate(t, s, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
	mustStoreCreate(t, s, "green", "2027-10-31T23:00:00Z", "2027-11-01T01:00:00Z")
	mustStoreCreate(t, s, "green", "2027-11-01T12:00:00Z", "2027-11-01T12:30:00Z")

	got := listDay(t, NewHandler(s), "green", "2027-11-01")

	want := []string{"2027-10-31T23:00:00Z", "2027-11-01T09:00:00Z", "2027-11-01T12:00:00Z", "2027-11-01T15:00:00Z"}
	if len(got) != len(want) {
		t.Fatalf("len(bookings) = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Start != w {
			t.Fatalf("bookings[%d].start = %q, want %q", i, got[i].Start, w)
		}
	}
}

func TestListBookings_OtherRooms_Excluded(t *testing.T) {
	s := NewStore()
	b := mustStoreCreate(t, s, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
	for _, room := range []string{"blue", "Green", "green "} {
		mustStoreCreate(t, s, room, "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
	}

	assertOnlyBooking(t, listDay(t, NewHandler(s), "green", "2027-11-01"),
		respBooking{b.ID, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"})
}

func TestListBookings_ExtraQueryParams_Ignored(t *testing.T) {
	s := NewStore()
	b := mustStoreCreate(t, s, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")

	rec := do(t, NewHandler(s), http.MethodGet, "/bookings?room=green&date=2027-11-01&limit=0&foo=bar", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var body struct {
		Bookings []json.RawMessage `json:"bookings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("list body: %v; body: %s", err, rec.Body)
	}
	if len(body.Bookings) != 1 {
		t.Fatalf("len(bookings) = %d, want 1; body: %s", len(body.Bookings), rec.Body)
	}
	if got, want := decodeBooking(t, body.Bookings[0]), (respBooking{b.ID, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"}); got != want {
		t.Fatalf("booking = %+v, want %+v", got, want)
	}
}

func TestListBookings_NoBookings_EmptyArray(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, s *Store)
	}{
		{"броней нет", func(*testing.T, *Store) {}},
		{"у комнаты только бронь в другие сутки", func(t *testing.T, s *Store) {
			mustStoreCreate(t, s, "green", "2027-11-02T09:00:00Z", "2027-11-02T10:00:00Z")
		}},
		{"бронь в эти сутки только у другой комнаты", func(t *testing.T, s *Store) {
			mustStoreCreate(t, s, "blue", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()
			tt.setup(t, s)

			rec := do(t, NewHandler(s), http.MethodGet, "/bookings?room=green&date=2027-11-01", "")

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
			}
			assertJSONContentType(t, rec)
			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not a JSON object: %v; body: %s", err, rec.Body)
			}
			if got := string(body["bookings"]); got != "[]" {
				t.Fatalf("bookings = %s, want []", got)
			}
		})
	}
}

func TestListBookings_InvalidParams_Returns400(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"параметров нет", ""},
		{"room отсутствует", "?date=2027-11-01"},
		{"room пустой", "?room=&date=2027-11-01"},
		{"room только из пробелов", "?room=%20%20%20&date=2027-11-01"},
		{"room — неразрывный пробел", "?room=%C2%A0&date=2027-11-01"},
		{"date отсутствует", "?room=green"},
		{"date пустой", "?room=green&date="},
		{"день одной цифрой", "?room=green&date=2027-11-1"},
		{"несуществующая дата", "?room=green&date=2027-02-30"},
		{"другой формат даты", "?room=green&date=01.11.2027"},
		{"без дефисов", "?room=green&date=20271101"},
		{"дата со временем", "?room=green&date=2027-11-01T00:00:00Z"},
		{"пробел перед датой", "?room=green&date=%202027-11-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()
			b := mustStoreCreate(t, s, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
			h := NewHandler(s)

			assertErrorResponse(t, do(t, h, http.MethodGet, "/bookings"+tt.query, ""), http.StatusBadRequest)
			assertOnlyBooking(t, listDay(t, h, "green", "2027-11-01"),
				respBooking{b.ID, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"})
		})
	}
}

func TestCreateBooking_Valid_ReturnsExactlyBookingFields(t *testing.T) {
	h := NewHandler(NewStore())

	rec := do(t, h, http.MethodPost, "/bookings",
		`{"room": "green", "start": "2027-11-01T09:00:00Z", "end": "2027-11-01T10:00:00Z"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body)
	}
	assertJSONContentType(t, rec)
	got := decodeBooking(t, rec.Body.Bytes())
	if got.ID == "" {
		t.Fatalf("id is empty")
	}
	if want := (respBooking{got.ID, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z"}); got != want {
		t.Fatalf("booking = %+v, want %+v", got, want)
	}
}

func TestCreateBooking_ZeroOffset_NormalizedToZ(t *testing.T) {
	tests := []struct {
		start, end         string
		wantStart, wantEnd string
		date               string
	}{
		{"2027-11-01T09:00:00+00:00", "2027-11-01T10:00:00+00:00", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z", "2027-11-01"},
		{"2027-11-02T09:00:00-00:00", "2027-11-02T10:00:00-00:00", "2027-11-02T09:00:00Z", "2027-11-02T10:00:00Z", "2027-11-02"},
	}
	h := NewHandler(NewStore())
	for _, tt := range tests {
		t.Run(tt.start, func(t *testing.T) {
			got := mustCreate(t, h, "green", tt.start, tt.end)

			if got.Start != tt.wantStart || got.End != tt.wantEnd {
				t.Fatalf("interval = %s–%s, want %s–%s", got.Start, got.End, tt.wantStart, tt.wantEnd)
			}
			assertOnlyBooking(t, listDay(t, h, "green", tt.date), respBooking{got.ID, "green", tt.wantStart, tt.wantEnd})
		})
	}
}

func TestCreateBooking_FractionalSeconds_Preserved(t *testing.T) {
	h := NewHandler(NewStore())

	rec := do(t, h, http.MethodPost, "/bookings",
		`{"room": "green", "start": "2027-11-01T09:00:00.5Z", "end": "2027-11-01T10:00:00.123456789+00:00"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body)
	}
	got := decodeBooking(t, rec.Body.Bytes())
	want := respBooking{got.ID, "green", "2027-11-01T09:00:00.5Z", "2027-11-01T10:00:00.123456789Z"}
	if got != want {
		t.Fatalf("booking = %+v, want %+v", got, want)
	}
	assertOnlyBooking(t, listDay(t, h, "green", "2027-11-01"), want)
}

func TestCreateBooking_Several_UniqueIDs(t *testing.T) {
	h := NewHandler(NewStore())

	g1 := mustCreate(t, h, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
	g2 := mustCreate(t, h, "green", "2027-11-01T10:00:00Z", "2027-11-01T11:00:00Z")
	b1 := mustCreate(t, h, "blue", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")

	if g1.ID == "" || g2.ID == "" || b1.ID == "" {
		t.Fatalf("empty id among %q, %q, %q", g1.ID, g2.ID, b1.ID)
	}
	if g1.ID == g2.ID || g1.ID == b1.ID || g2.ID == b1.ID {
		t.Fatalf("ids are not pairwise distinct: %q, %q, %q", g1.ID, g2.ID, b1.ID)
	}
	green := listDay(t, h, "green", "2027-11-01")
	if len(green) != 2 || green[0] != g1 || green[1] != g2 {
		t.Fatalf("green bookings = %+v, want [%+v %+v]", green, g1, g2)
	}
	assertOnlyBooking(t, listDay(t, h, "blue", "2027-11-01"), b1)
}

func TestCreateBooking_Valid_Returns201AndListed(t *testing.T) {
	h := NewHandler(NewStore())

	got := mustCreate(t, h, "green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")

	if got.ID == "" || got.Room != "green" || got.Start != "2027-11-01T09:00:00Z" || got.End != "2027-11-01T10:00:00Z" {
		t.Fatalf("booking = %+v", got)
	}
	assertOnlyBooking(t, listDay(t, h, "green", "2027-11-01"), got)
}

func TestCreateBooking_RoomVerbatim_StoredAsIs(t *testing.T) {
	h := NewHandler(NewStore())

	got := mustCreate(t, h, " Переговорная 1 ", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")

	if got.Room != " Переговорная 1 " {
		t.Fatalf("room = %q, want %q", got.Room, " Переговорная 1 ")
	}
	assertOnlyBooking(t, listDay(t, h, " Переговорная 1 ", "2027-11-01"), got)
	for _, room := range []string{"Переговорная 1", " переговорная 1 "} {
		if other := listDay(t, h, room, "2027-11-01"); len(other) != 0 {
			t.Fatalf("bookings of room %q = %+v, want empty", room, other)
		}
	}
}

func TestCreateBooking_SingleNonSpaceRune_Returns201(t *testing.T) {
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
			got := mustCreate(t, NewHandler(NewStore()), tt.room, "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")

			if got.Room != tt.room {
				t.Fatalf("room = %q, want %q", got.Room, tt.room)
			}
		})
	}
}

func TestCreateBooking_ExtraFields_Ignored(t *testing.T) {
	h := NewHandler(NewStore())

	rec := do(t, h, http.MethodPost, "/bookings",
		`{"room": "green", "start": "2027-11-01T09:00:00Z", "end": "2027-11-01T10:00:00Z", "id": "client-id", "note": "планёрка", "Room": "blue"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body)
	}
	got := decodeBooking(t, rec.Body.Bytes())
	if got.ID == "client-id" {
		t.Fatalf("id = %q, want a server-generated id", got.ID)
	}
	if got.Room != "green" {
		t.Fatalf("room = %q, want %q", got.Room, "green")
	}
	assertOnlyBooking(t, listDay(t, h, "green", "2027-11-01"), got)
	if blue := listDay(t, h, "blue", "2027-11-01"); len(blue) != 0 {
		t.Fatalf("blue bookings = %+v, want empty", blue)
	}
}

func TestCreateBooking_PastAndAnyDuration_Returns201(t *testing.T) {
	tests := []struct {
		name  string
		start string
		end   string
		date  string
	}{
		{"бронь в прошлом", "2020-01-01T09:00:00Z", "2020-01-01T10:00:00Z", "2020-01-01"},
		{"длительность 1 наносекунда", "2027-11-01T09:00:00Z", "2027-11-01T09:00:00.000000001Z", "2027-11-01"},
		{"длительность больше года", "2027-12-01T00:00:00Z", "2028-12-01T00:00:00Z", "2027-12-01"},
	}
	h := NewHandler(NewStore())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustCreate(t, h, "green", tt.start, tt.end)

			if got.Start != tt.start || got.End != tt.end {
				t.Fatalf("interval = %s–%s, want %s–%s", got.Start, got.End, tt.start, tt.end)
			}
			assertOnlyBooking(t, listDay(t, h, "green", tt.date), got)
		})
	}
}

// setupExistingGreen creates the booking that rejected requests must leave untouched.
func setupExistingGreen(t *testing.T, start, end string) (*Store, http.Handler, respBooking) {
	t.Helper()
	s := NewStore()
	b := mustStoreCreate(t, s, "green", start, end)
	return s, NewHandler(s), respBooking{b.ID, "green", start, end}
}

func TestCreateBooking_NotJSONObject_Returns400(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"пустое тело", ""},
		{"не JSON", "not json"},
		{"оборванный JSON", `{"room": "green", "start": "2027-11-01T09:00:00Z"`},
		{"мусор после объекта", `{"room": "green", "start": "2027-11-01T09:00:00Z", "end": "2027-11-01T10:00:00Z"} x`},
		{"два JSON-значения", `{"room": "green", "start": "2027-11-01T09:00:00Z", "end": "2027-11-01T10:00:00Z"}{}`},
		{"null вместо объекта", "null"},
		{"массив вместо объекта", `[{"room": "green", "start": "2027-11-01T09:00:00Z", "end": "2027-11-01T10:00:00Z"}]`},
		{"строка вместо объекта", `"green"`},
		{"число вместо объекта", "42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, h, existing := setupExistingGreen(t, "2027-11-01T12:00:00Z", "2027-11-01T13:00:00Z")

			assertErrorResponse(t, do(t, h, http.MethodPost, "/bookings", tt.body), http.StatusBadRequest)
			assertNothingCreated(t, s, h, "2027-11-01", existing)
		})
	}
}

func TestCreateBooking_InvalidRoom_Returns400(t *testing.T) {
	tests := []struct {
		name string
		room string
	}{
		{"ключа room нет", ""},
		{"null", `"room": null, `},
		{"пустая строка", `"room": "", `},
		{"только пробелы", `"room": "   ", `},
		{"табуляция и перевод строки", `"room": "\t\n", `},
		{"неразрывный пробел", `"room": "\u00a0", `},
		{"число", `"room": 5, `},
		{"массив", `"room": ["green"], `},
		{"ключ в другом регистре", `"ROOM": "green", `},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, h, existing := setupExistingGreen(t, "2027-11-01T12:00:00Z", "2027-11-01T13:00:00Z")
			body := "{" + tt.room + `"start": "2027-11-01T09:00:00Z", "end": "2027-11-01T10:00:00Z"}`

			assertErrorResponse(t, do(t, h, http.MethodPost, "/bookings", body), http.StatusBadRequest)
			assertNothingCreated(t, s, h, "2027-11-01", existing)
		})
	}
}

func TestCreateBooking_InvalidTimes_Returns400(t *testing.T) {
	// start and end hold raw JSON values; an empty string means the key is absent.
	tests := []struct {
		name  string
		start string
		end   string
	}{
		{"start отсутствует", "", `"2027-11-01T10:00:00Z"`},
		{"end отсутствует", `"2027-11-01T09:00:00Z"`, ""},
		{"start равен null", "null", `"2027-11-01T10:00:00Z"`},
		{"end равен null", `"2027-11-01T09:00:00Z"`, "null"},
		{"start — число", "1824627600", `"2027-11-01T10:00:00Z"`},
		{"start — пустая строка", `""`, `"2027-11-01T10:00:00Z"`},
		{"только дата", `"2027-11-01"`, `"2027-11-01T10:00:00Z"`},
		{"без смещения", `"2027-11-01T09:00:00"`, `"2027-11-01T10:00:00Z"`},
		{"пробел вместо T", `"2027-11-01 09:00:00Z"`, `"2027-11-01T10:00:00Z"`},
		{"строчные t и z", `"2027-11-01t09:00:00z"`, `"2027-11-01T10:00:00Z"`},
		{"смещение без двоеточия", `"2027-11-01T09:00:00+0000"`, `"2027-11-01T10:00:00Z"`},
		{"несуществующая дата", `"2027-02-30T09:00:00Z"`, `"2027-03-01T10:00:00Z"`},
		{"ненулевое смещение start", `"2027-11-01T09:00:00+03:00"`, `"2027-11-01T10:00:00Z"`},
		{"ненулевое смещение end", `"2027-11-01T09:00:00Z"`, `"2027-11-01T10:00:00-05:00"`},
		{"смещение в одну минуту", `"2027-11-01T09:00:00Z"`, `"2027-11-01T10:00:00+00:01"`},
		{"end равен start", `"2027-11-01T09:00:00Z"`, `"2027-11-01T09:00:00Z"`},
		{"end равен start в другой записи", `"2027-11-01T09:00:00Z"`, `"2027-11-01T09:00:00.000+00:00"`},
		{"end раньше start на 1 наносекунду", `"2027-11-01T09:00:00.000000001Z"`, `"2027-11-01T09:00:00Z"`},
		{"end раньше start на час", `"2027-11-01T09:00:00Z"`, `"2027-11-01T08:00:00Z"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, h, existing := setupExistingGreen(t, "2027-11-01T12:00:00Z", "2027-11-01T13:00:00Z")
			fields := []string{`"room": "green"`}
			if tt.start != "" {
				fields = append(fields, `"start": `+tt.start)
			}
			if tt.end != "" {
				fields = append(fields, `"end": `+tt.end)
			}
			body := "{" + strings.Join(fields, ", ") + "}"

			assertErrorResponse(t, do(t, h, http.MethodPost, "/bookings", body), http.StatusBadRequest)
			assertNothingCreated(t, s, h, "2027-11-01", existing)
		})
	}
}

func TestCreateBooking_Overlap_Returns409(t *testing.T) {
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
		{"общее начало", "2027-11-01T09:00:00Z", "2027-11-01T09:30:00Z"},
		{"общий конец", "2027-11-01T09:30:00Z", "2027-11-01T10:00:00Z"},
		{"пересечение на 1 с у конца", "2027-11-01T09:59:59Z", "2027-11-01T11:00:00Z"},
		{"пересечение на 1 с у начала", "2027-11-01T08:00:00Z", "2027-11-01T09:00:01Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, h, existing := setupExistingGreen(t, "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")

			rec := do(t, h, http.MethodPost, "/bookings", createBody("green", tt.start, tt.end))

			assertErrorResponse(t, rec, http.StatusConflict)
			assertNothingCreated(t, s, h, "2027-11-01", existing)
		})
	}
}

func TestCreateBooking_Touching_Returns201(t *testing.T) {
	_, h, existing := setupExistingGreen(t, "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")

	after := mustCreate(t, h, "green", "2027-11-01T10:00:00Z", "2027-11-01T11:00:00Z")
	before := mustCreate(t, h, "green", "2027-11-01T08:00:00Z", "2027-11-01T09:00:00Z")

	got := listDay(t, h, "green", "2027-11-01")
	want := []respBooking{before, existing, after}
	if len(got) != len(want) {
		t.Fatalf("bookings = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bookings[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCreateBooking_SubsecondBoundary_ConflictsPrecisely(t *testing.T) {
	_, h, existing := setupExistingGreen(t, "2027-11-01T09:00:00Z", "2027-11-01T10:00:00.5Z")

	rec := do(t, h, http.MethodPost, "/bookings", createBody("green", "2027-11-01T10:00:00Z", "2027-11-01T11:00:00Z"))
	assertErrorResponse(t, rec, http.StatusConflict)
	touching := mustCreate(t, h, "green", "2027-11-01T10:00:00.5Z", "2027-11-01T11:00:00Z")

	got := listDay(t, h, "green", "2027-11-01")
	if len(got) != 2 || got[0] != existing || got[1] != touching {
		t.Fatalf("bookings = %+v, want [%+v %+v]", got, existing, touching)
	}
}

func TestCreateBooking_OtherRooms_NoConflict(t *testing.T) {
	_, h, existing := setupExistingGreen(t, "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
	rooms := []string{"blue", "Green", "GREEN", " green", "green "}

	created := make([]respBooking, 0, len(rooms))
	for _, room := range rooms {
		c := mustCreate(t, h, room, "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
		if c.Room != room || c.Start != "2027-11-01T09:00:00Z" || c.End != "2027-11-01T10:00:00Z" {
			t.Fatalf("created booking = %+v, want room %q 2027-11-01T09:00:00Z–2027-11-01T10:00:00Z", c, room)
		}
		created = append(created, c)
	}

	assertOnlyBooking(t, listDay(t, h, "green", "2027-11-01"), existing)
	for i, room := range rooms {
		assertOnlyBooking(t, listDay(t, h, room, "2027-11-01"), created[i])
	}
}

// postConcurrently builds all requests first and releases them together to
// maximize contention on the store.
func postConcurrently(h http.Handler, bodies []string) []*httptest.ResponseRecorder {
	recs := make([]*httptest.ResponseRecorder, len(bodies))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, body := range bodies {
		req := httptest.NewRequest(http.MethodPost, "/bookings", strings.NewReader(body))
		rec := httptest.NewRecorder()
		recs[i] = rec
		wg.Go(func() {
			<-start
			h.ServeHTTP(rec, req)
		})
	}
	close(start)
	wg.Wait()
	return recs
}

func assertExactlyOneCreated(t *testing.T, h http.Handler, recs []*httptest.ResponseRecorder) {
	t.Helper()
	var winners []respBooking
	for _, rec := range recs {
		if rec.Code == http.StatusCreated {
			winners = append(winners, decodeBooking(t, rec.Body.Bytes()))
			continue
		}
		assertErrorResponse(t, rec, http.StatusConflict)
	}
	if len(winners) != 1 {
		t.Fatalf("created %d bookings, want exactly 1: %+v", len(winners), winners)
	}
	assertOnlyBooking(t, listDay(t, h, "green", "2027-11-01"), winners[0])
}

func TestCreateBooking_ConcurrentIdentical_ExactlyOneCreated(t *testing.T) {
	h := NewHandler(NewStore())
	bodies := make([]string, 50)
	for i := range bodies {
		bodies[i] = createBody("green", "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")
	}

	assertExactlyOneCreated(t, h, postConcurrently(h, bodies))
}

func TestCreateBooking_ConcurrentOverlapping_ExactlyOneCreated(t *testing.T) {
	h := NewHandler(NewStore())
	base := time.Date(2027, 11, 1, 9, 0, 0, 0, time.UTC)
	bodies := make([]string, 50)
	for i := range bodies {
		start := base.Add(time.Duration(i) * time.Minute)
		bodies[i] = createBody("green", start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339))
	}

	assertExactlyOneCreated(t, h, postConcurrently(h, bodies))
}

func assertAllowsGetAndPost(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	allowed := map[string]bool{}
	for m := range strings.SplitSeq(rec.Header().Get("Allow"), ",") {
		allowed[strings.TrimSpace(m)] = true
	}
	if !allowed[http.MethodGet] || !allowed[http.MethodPost] {
		t.Fatalf("Allow = %q, want GET and POST", rec.Header().Get("Allow"))
	}
}

func TestErrors_AllKinds_JSONFormat(t *testing.T) {
	tests := []struct {
		method string
		target string
		body   string
		status int
	}{
		{http.MethodPost, "/bookings", "not json", http.StatusBadRequest},
		{http.MethodGet, "/bookings?room=green", "", http.StatusBadRequest},
		{http.MethodPost, "/bookings", createBody("green", "2027-11-01T09:30:00Z", "2027-11-01T10:30:00Z"), http.StatusConflict},
		{http.MethodPut, "/bookings", "", http.StatusMethodNotAllowed},
		{http.MethodGet, "/bookings/123", "", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.target, func(t *testing.T) {
			_, h, _ := setupExistingGreen(t, "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")

			assertErrorResponse(t, do(t, h, tt.method, tt.target, tt.body), tt.status)
		})
	}
}

func TestBookings_UnsupportedMethod_Returns405(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			s, h, existing := setupExistingGreen(t, "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")

			rec := do(t, h, method, "/bookings", createBody("green", "2027-11-01T11:00:00Z", "2027-11-01T12:00:00Z"))

			assertErrorResponse(t, rec, http.StatusMethodNotAllowed)
			assertAllowsGetAndPost(t, rec)
			assertNothingCreated(t, s, h, "2027-11-01", existing)
		})
	}
}

func TestBookings_Head_Returns405(t *testing.T) {
	h := NewHandler(NewStore())

	rec := do(t, h, http.MethodHead, "/bookings?room=green&date=2027-11-01", "")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	assertAllowsGetAndPost(t, rec)
	assertJSONContentType(t, rec)
}

func TestUnknownPath_Returns404(t *testing.T) {
	tests := []struct {
		method string
		target string
	}{
		{http.MethodGet, "/"},
		{http.MethodGet, "/bookings/123"},
		{http.MethodGet, "/bookings/?room=green&date=2027-11-01"},
		{http.MethodPost, "/booking"},
		{http.MethodPost, "/bookings/"},
		{http.MethodGet, "//bookings?room=green&date=2027-11-01"},
		{http.MethodGet, "/x/../bookings?room=green&date=2027-11-01"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.target, func(t *testing.T) {
			s, h, existing := setupExistingGreen(t, "2027-11-01T09:00:00Z", "2027-11-01T10:00:00Z")

			rec := do(t, h, tt.method, tt.target, createBody("green", "2027-11-01T11:00:00Z", "2027-11-01T12:00:00Z"))

			assertErrorResponse(t, rec, http.StatusNotFound)
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Fatalf("Location = %q, want no redirect", loc)
			}
			assertNothingCreated(t, s, h, "2027-11-01", existing)
		})
	}
}
