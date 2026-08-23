package booking

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var testNow = time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

func newServerAtTime(now func() time.Time) *http.ServeMux {
	mux := http.NewServeMux()
	NewHandler(NewStore(now, 10*time.Minute)).Register(mux)
	return mux
}

func newServer() *http.ServeMux {
	return newServerAtTime(func() time.Time { return testNow })
}

func post(t *testing.T, mux *http.ServeMux, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/bookings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func get(t *testing.T, mux *http.ServeMux, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/bookings?"+query, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func del(t *testing.T, mux *http.ServeMux, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/bookings/"+id, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func createBooking(t *testing.T, mux *http.ServeMux, body string) Booking {
	t.Helper()
	rec := post(t, mux, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("подготовка брони %s: код %d, тело %s", body, rec.Code, rec.Body.String())
	}
	var b Booking
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("разобрать ответ: %v", err)
	}
	return b
}

func TestCreateSuccess(t *testing.T) {
	mux := newServer()

	rec := post(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var got Booking
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разобрать ответ: %v", err)
	}
	if len(got.ID) != 16 {
		t.Fatalf("длина id = %d, хотим 16 (%q)", len(got.ID), got.ID)
	}
	if got.Room != "green" {
		t.Fatalf("room = %q, хотим %q", got.Room, "green")
	}
	if got.Start.Format("15:04") != "10:00" || got.End.Format("15:04") != "11:00" {
		t.Fatalf("интервал = %s–%s, хотим 10:00–11:00", got.Start, got.End)
	}
}

func TestCreateSecondBooking(t *testing.T) {
	tests := []struct {
		name  string
		first string
		body  string
		want  int
	}{
		{
			name:  "полное совпадение",
			first: `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`,
			body:  `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`,
			want:  http.StatusConflict,
		},
		{
			name:  "частичное пересечение слева",
			first: `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`,
			body:  `{"room":"green","start":"2026-08-24T09:30:00Z","end":"2026-08-24T10:30:00Z"}`,
			want:  http.StatusConflict,
		},
		{
			name:  "частичное пересечение справа",
			first: `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`,
			body:  `{"room":"green","start":"2026-08-24T10:30:00Z","end":"2026-08-24T12:00:00Z"}`,
			want:  http.StatusConflict,
		},
		{
			name:  "вложенный интервал",
			first: `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T12:00:00Z"}`,
			body:  `{"room":"green","start":"2026-08-24T10:30:00Z","end":"2026-08-24T11:00:00Z"}`,
			want:  http.StatusConflict,
		},
		{
			name:  "объемлющий интервал",
			first: `{"room":"green","start":"2026-08-24T10:30:00Z","end":"2026-08-24T11:00:00Z"}`,
			body:  `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T12:00:00Z"}`,
			want:  http.StatusConflict,
		},
		{
			name:  "то же время в другой комнате",
			first: `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`,
			body:  `{"room":"red","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`,
			want:  http.StatusCreated,
		},
		{
			name:  "непересекающийся интервал",
			first: `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`,
			body:  `{"room":"green","start":"2026-08-24T14:00:00Z","end":"2026-08-24T15:00:00Z"}`,
			want:  http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newServer()
			if rec := post(t, mux, tt.first); rec.Code != http.StatusCreated {
				t.Fatalf("первая бронь: код %d, тело %s", rec.Code, rec.Body.String())
			}

			rec := post(t, mux, tt.body)
			if rec.Code != tt.want {
				t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, tt.want, rec.Body.String())
			}
			if tt.want == http.StatusConflict {
				assertErrorBody(t, rec)
			}
		})
	}
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "невалидный JSON", body: `{"room": "green"`},
		{name: "пустое тело", body: ``},
		{name: "пустая комната", body: `{"room":"","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`},
		{name: "нет комнаты", body: `{"start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`},
		{name: "нет start", body: `{"room":"green","end":"2026-08-24T11:00:00Z"}`},
		{name: "нет end", body: `{"room":"green","start":"2026-08-24T10:00:00Z"}`},
		{name: "неизвестный формат start", body: `{"room":"green","start":"24.08.2026 10:00","end":"2026-08-24T11:00:00Z"}`},
		{name: "неизвестный формат end", body: `{"room":"green","start":"2026-08-24T10:00:00Z","end":"завтра"}`},
		{name: "start не в UTC", body: `{"room":"green","start":"2026-08-24T10:00:00+03:00","end":"2026-08-24T11:00:00Z"}`},
		{name: "end не в UTC", body: `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T14:00:00+03:00"}`},
		{name: "end равен start", body: `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T10:00:00Z"}`},
		{name: "end меньше start", body: `{"room":"green","start":"2026-08-24T11:00:00Z","end":"2026-08-24T10:00:00Z"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newServer()
			rec := post(t, mux, tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			assertErrorBody(t, rec)
		})
	}
}

func TestListSortedAndFiltered(t *testing.T) {
	mux := newServer()
	seed := []string{
		`{"room":"green","start":"2026-08-24T15:00:00Z","end":"2026-08-24T16:00:00Z"}`,
		`{"room":"green","start":"2026-08-24T09:00:00Z","end":"2026-08-24T09:30:00Z"}`,
		`{"room":"green","start":"2026-08-24T12:00:00Z","end":"2026-08-24T13:00:00Z"}`,
		`{"room":"green","start":"2026-08-25T08:00:00Z","end":"2026-08-25T09:00:00Z"}`,
		`{"room":"red","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`,
	}
	for _, body := range seed {
		if rec := post(t, mux, body); rec.Code != http.StatusCreated {
			t.Fatalf("подготовка брони %s: код %d, тело %s", body, rec.Code, rec.Body.String())
		}
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "сутки комнаты green",
			query: "room=green&date=2026-08-24",
			want:  []string{"09:00", "12:00", "15:00"},
		},
		{
			name:  "следующие сутки",
			query: "room=green&date=2026-08-25",
			want:  []string{"08:00"},
		},
		{
			name:  "сутки без броней",
			query: "room=green&date=2026-08-26",
			want:  []string{},
		},
		{
			name:  "другая комната",
			query: "room=red&date=2026-08-24",
			want:  []string{"10:00"},
		},
		{
			name:  "неизвестная комната",
			query: "room=blue&date=2026-08-24",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, mux, tt.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusOK, rec.Body.String())
			}

			var got listResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("разобрать ответ: %v", err)
			}
			if len(got.Bookings) != len(tt.want) {
				t.Fatalf("броней = %d, хотим %d (%s)", len(got.Bookings), len(tt.want), rec.Body.String())
			}
			for i, want := range tt.want {
				if got := got.Bookings[i].Start.Format("15:04"); got != want {
					t.Fatalf("бронь %d начинается в %s, хотим %s", i, got, want)
				}
			}
		})
	}
}

func TestListValidation(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "нет room", query: "date=2026-08-24"},
		{name: "пустой room", query: "room=&date=2026-08-24"},
		{name: "нет date", query: "room=green"},
		{name: "неразборчивый date", query: "room=green&date=24-08-2026"},
		{name: "date с временем", query: "room=green&date=2026-08-24T10:00:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newServer()
			rec := get(t, mux, tt.query)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			assertErrorBody(t, rec)
		})
	}
}

func TestCreateConcurrent(t *testing.T) {
	mux := newServer()

	const goroutines = 50
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		other   []int
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			rec := post(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)

			mu.Lock()
			defer mu.Unlock()
			switch rec.Code {
			case http.StatusCreated:
				created++
			case http.StatusConflict:
			default:
				other = append(other, rec.Code)
			}
		}()
	}
	wg.Wait()

	if created != 1 {
		t.Fatalf("успешных броней = %d, хотим 1", created)
	}
	if len(other) != 0 {
		t.Fatalf("неожиданные коды ответов: %v", other)
	}
}

func TestDeleteSuccess(t *testing.T) {
	mux := newServer()
	b := createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)

	rec := del(t, mux, b.ID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("тело ответа не пустое: %s", rec.Body.String())
	}

	list := get(t, mux, "room=green&date=2026-08-24")
	var got listResponse
	if err := json.Unmarshal(list.Body.Bytes(), &got); err != nil {
		t.Fatalf("разобрать ответ: %v", err)
	}
	if len(got.Bookings) != 0 {
		t.Fatalf("после отмены осталось броней = %d, хотим 0 (%s)", len(got.Bookings), list.Body.String())
	}
}

func TestDeleteNotFound(t *testing.T) {
	tests := []struct {
		name string
		id   func(t *testing.T, mux *http.ServeMux) string
	}{
		{
			name: "неизвестный id",
			id:   func(*testing.T, *http.ServeMux) string { return "0123456789abcdef" },
		},
		{
			name: "id не в формате хранилища",
			id:   func(*testing.T, *http.ServeMux) string { return "не-id" },
		},
		{
			name: "повторная отмена",
			id: func(t *testing.T, mux *http.ServeMux) string {
				b := createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)
				if rec := del(t, mux, b.ID); rec.Code != http.StatusNoContent {
					t.Fatalf("первая отмена: код %d, тело %s", rec.Code, rec.Body.String())
				}
				return b.ID
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newServer()
			rec := del(t, mux, tt.id(t, mux))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusNotFound, rec.Body.String())
			}
			assertErrorBody(t, rec)
		})
	}
}

func TestDeleteFreesInterval(t *testing.T) {
	mux := newServer()
	body := `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`
	b := createBooking(t, mux, body)

	if rec := post(t, mux, body); rec.Code != http.StatusConflict {
		t.Fatalf("до отмены: код %d, хотим %d; тело %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if rec := del(t, mux, b.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("отмена: код %d, тело %s", rec.Code, rec.Body.String())
	}
	if rec := post(t, mux, body); rec.Code != http.StatusCreated {
		t.Fatalf("после отмены: код %d, хотим %d; тело %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestDeleteKeepsOtherBookings(t *testing.T) {
	mux := newServer()
	target := createBooking(t, mux, `{"room":"green","start":"2026-08-24T12:00:00Z","end":"2026-08-24T13:00:00Z"}`)
	createBooking(t, mux, `{"room":"green","start":"2026-08-24T09:00:00Z","end":"2026-08-24T10:00:00Z"}`)
	createBooking(t, mux, `{"room":"green","start":"2026-08-24T15:00:00Z","end":"2026-08-24T16:00:00Z"}`)
	createBooking(t, mux, `{"room":"red","start":"2026-08-24T12:00:00Z","end":"2026-08-24T13:00:00Z"}`)

	if rec := del(t, mux, target.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("отмена: код %d, тело %s", rec.Code, rec.Body.String())
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "комната с отменённой бронью", query: "room=green&date=2026-08-24", want: []string{"09:00", "15:00"}},
		{name: "соседняя комната", query: "room=red&date=2026-08-24", want: []string{"12:00"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, mux, tt.query)
			var got listResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("разобрать ответ: %v", err)
			}
			if len(got.Bookings) != len(tt.want) {
				t.Fatalf("броней = %d, хотим %d (%s)", len(got.Bookings), len(tt.want), rec.Body.String())
			}
			for i, want := range tt.want {
				if got := got.Bookings[i].Start.Format("15:04"); got != want {
					t.Fatalf("бронь %d начинается в %s, хотим %s", i, got, want)
				}
			}
		})
	}
}

func TestDeleteConcurrent(t *testing.T) {
	mux := newServer()
	b := createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)

	const goroutines = 50
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		deleted int
		other   []int
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			rec := del(t, mux, b.ID)

			mu.Lock()
			defer mu.Unlock()
			switch rec.Code {
			case http.StatusNoContent:
				deleted++
			case http.StatusNotFound:
			default:
				other = append(other, rec.Code)
			}
		}()
	}
	wg.Wait()

	if deleted != 1 {
		t.Fatalf("успешных отмен = %d, хотим 1", deleted)
	}
	if len(other) != 0 {
		t.Fatalf("неожиданные коды ответов: %v", other)
	}
}

func TestCreate_OverCancelled_CreatedAndSingleInList(t *testing.T) {
	mux := newServer()
	body := `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`
	a := createBooking(t, mux, body)
	if rec := del(t, mux, a.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("отмена: код %d, тело %s", rec.Code, rec.Body.String())
	}

	b := createBooking(t, mux, body)

	list := get(t, mux, "room=green&date=2026-08-24")
	var got listResponse
	if err := json.Unmarshal(list.Body.Bytes(), &got); err != nil {
		t.Fatalf("разобрать ответ: %v", err)
	}
	if len(got.Bookings) != 1 {
		t.Fatalf("броней в выдаче = %d, хотим 1 (%s)", len(got.Bookings), list.Body.String())
	}
	if got.Bookings[0].ID != b.ID {
		t.Fatalf("в выдаче бронь %s, хотим %s", got.Bookings[0].ID, b.ID)
	}
}

func restore(t *testing.T, mux *http.ServeMux, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/bookings/"+id+"/restore", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func cancelAndAdvance(t *testing.T, mux *http.ServeMux, now *time.Time, id string, elapsed time.Duration) {
	t.Helper()
	if rec := del(t, mux, id); rec.Code != http.StatusNoContent {
		t.Fatalf("отмена: код %d, тело %s", rec.Code, rec.Body.String())
	}
	*now = testNow.Add(elapsed)
}

func listCount(t *testing.T, mux *http.ServeMux, query string) (listResponse, int) {
	t.Helper()
	rec := get(t, mux, query)
	var got listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разобрать ответ: %v", err)
	}
	return got, len(got.Bookings)
}

func TestRestore_WithinWindow_RestoredAndListed(t *testing.T) {
	now := testNow
	mux := newServerAtTime(func() time.Time { return now })
	b := createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)
	cancelAndAdvance(t, mux, &now, b.ID, 5*time.Minute)

	rec := restore(t, mux, b.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("разобрать ответ: %v", err)
	}
	if len(raw) != 4 {
		t.Fatalf("полей в ответе = %d, хотим 4 (id, room, start, end): %s", len(raw), rec.Body.String())
	}
	var got Booking
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разобрать ответ: %v", err)
	}
	if got.ID != b.ID || got.Room != "green" || got.Start.Format(time.RFC3339) != "2026-08-24T10:00:00Z" || got.End.Format(time.RFC3339) != "2026-08-24T11:00:00Z" {
		t.Fatalf("восстановленная бронь = %+v, хотим исходную %s green 10:00–11:00", got, b.ID)
	}

	if _, count := listCount(t, mux, "room=green&date=2026-08-24"); count != 1 {
		t.Fatalf("броней в выдаче = %d, хотим 1", count)
	}
}

func TestRestore_ExactlyAtWindowEdge_Restored(t *testing.T) {
	now := testNow
	mux := newServerAtTime(func() time.Time { return now })
	b := createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)
	cancelAndAdvance(t, mux, &now, b.ID, 10*time.Minute)

	rec := restore(t, mux, b.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got Booking
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разобрать ответ: %v", err)
	}
	if got.ID != b.ID || got.Room != "green" {
		t.Fatalf("восстановленная бронь = %+v, хотим id %s green", got, b.ID)
	}
}

func TestRestore_UnknownId_NotFound(t *testing.T) {
	mux := newServer()

	rec := restore(t, mux, "deadbeef")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	assertErrorBody(t, rec)
}

func TestRestore_WindowExpired_Gone(t *testing.T) {
	now := testNow
	mux := newServerAtTime(func() time.Time { return now })
	b := createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)
	cancelAndAdvance(t, mux, &now, b.ID, 11*time.Minute)

	rec := restore(t, mux, b.ID)
	if rec.Code != http.StatusGone {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusGone, rec.Body.String())
	}
	assertErrorBody(t, rec)

	if _, count := listCount(t, mux, "room=green&date=2026-08-24"); count != 0 {
		t.Fatalf("броней в выдаче = %d, хотим 0", count)
	}
	if rec := restore(t, mux, b.ID); rec.Code != http.StatusGone {
		t.Fatalf("повторный restore: код %d, хотим %d", rec.Code, http.StatusGone)
	}
}

func TestRestore_ActiveBooking_BadRequest(t *testing.T) {
	now := testNow
	mux := newServerAtTime(func() time.Time { return now })
	b := createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)

	rec := restore(t, mux, b.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	assertErrorBody(t, rec)

	got, count := listCount(t, mux, "room=green&date=2026-08-24")
	if count != 1 {
		t.Fatalf("броней в выдаче = %d, хотим 1", count)
	}
	if got.Bookings[0].Start.Format("15:04") != "10:00" || got.Bookings[0].End.Format("15:04") != "11:00" {
		t.Fatalf("бронь изменилась: %s–%s", got.Bookings[0].Start, got.Bookings[0].End)
	}
}

func TestRestore_AfterSuccessfulRestore_BadRequest(t *testing.T) {
	now := testNow
	mux := newServerAtTime(func() time.Time { return now })
	b := createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)
	cancelAndAdvance(t, mux, &now, b.ID, time.Minute)
	if rec := restore(t, mux, b.ID); rec.Code != http.StatusOK {
		t.Fatalf("первый restore: код %d, тело %s", rec.Code, rec.Body.String())
	}

	rec := restore(t, mux, b.ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	assertErrorBody(t, rec)
}

func TestRestore_SlotTaken_ConflictAndNoSideEffects(t *testing.T) {
	now := testNow
	mux := newServerAtTime(func() time.Time { return now })
	a := createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)
	cancelAndAdvance(t, mux, &now, a.ID, time.Minute)
	b := createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:30:00Z","end":"2026-08-24T11:30:00Z"}`)
	now = testNow.Add(2 * time.Minute)

	rec := restore(t, mux, a.ID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	assertErrorBody(t, rec)

	got, count := listCount(t, mux, "room=green&date=2026-08-24")
	if count != 1 {
		t.Fatalf("броней в выдаче = %d, хотим 1", count)
	}
	if got.Bookings[0].ID != b.ID {
		t.Fatalf("в выдаче бронь %s, хотим %s", got.Bookings[0].ID, b.ID)
	}
	if rec := restore(t, mux, a.ID); rec.Code != http.StatusConflict {
		t.Fatalf("повторный restore: код %d, хотим %d", rec.Code, http.StatusConflict)
	}
}

func TestRestore_AdjacentBooking_Restored(t *testing.T) {
	now := testNow
	mux := newServerAtTime(func() time.Time { return now })
	a := createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)
	cancelAndAdvance(t, mux, &now, a.ID, time.Minute)
	createBooking(t, mux, `{"room":"green","start":"2026-08-24T11:00:00Z","end":"2026-08-24T12:00:00Z"}`)
	now = testNow.Add(2 * time.Minute)

	rec := restore(t, mux, a.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, count := listCount(t, mux, "room=green&date=2026-08-24"); count != 2 {
		t.Fatalf("броней в выдаче = %d, хотим 2", count)
	}
}

func TestRestore_AfterSecondCancel_NewWindow(t *testing.T) {
	now := testNow
	mux := newServerAtTime(func() time.Time { return now })
	b := createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)

	cancelAndAdvance(t, mux, &now, b.ID, time.Minute)
	if rec := restore(t, mux, b.ID); rec.Code != http.StatusOK {
		t.Fatalf("первый restore: код %d, тело %s", rec.Code, rec.Body.String())
	}

	now = testNow.Add(10 * time.Minute)
	cancelAndAdvance(t, mux, &now, b.ID, 0)
	now = testNow.Add(13 * time.Minute)

	rec := restore(t, mux, b.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got Booking
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разобрать ответ: %v", err)
	}
	if got.ID != b.ID || got.Room != "green" {
		t.Fatalf("восстановленная бронь = %+v, хотим id %s green", got, b.ID)
	}
}

func assertErrorBody(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("разобрать тело ошибки: %v (%s)", err, rec.Body.String())
	}
	if body["error"] == "" {
		t.Fatalf("тело ошибки без поля error: %s", rec.Body.String())
	}
}
