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

func newServer() *http.ServeMux {
	mux, _ := newServerWithStore()
	return mux
}

func newServerWithStore() (*http.ServeMux, *Store) {
	mux := http.NewServeMux()
	store := NewStore(10 * time.Minute)
	NewHandler(store).Register(mux)
	return mux, store
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

func restore(t *testing.T, mux *http.ServeMux, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/bookings/"+id+"/restore", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func cancelBooking(t *testing.T, mux *http.ServeMux, body string) Booking {
	t.Helper()
	b := createBooking(t, mux, body)
	if rec := del(t, mux, b.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("отмена брони %s: код %d, тело %s", body, rec.Code, rec.Body.String())
	}
	return b
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

func TestRestoreSuccess(t *testing.T) {
	mux := newServer()
	body := `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`
	b := cancelBooking(t, mux, body)

	rec := restore(t, mux, b.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got Booking
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разобрать ответ: %v", err)
	}
	if got != b {
		t.Fatalf("восстановленная бронь = %+v, хотим %+v", got, b)
	}

	list := get(t, mux, "room=green&date=2026-08-24")
	var lr listResponse
	if err := json.Unmarshal(list.Body.Bytes(), &lr); err != nil {
		t.Fatalf("разобрать список: %v", err)
	}
	if len(lr.Bookings) != 1 || lr.Bookings[0].ID != b.ID {
		t.Fatalf("после восстановления в списке %+v, хотим одна бронь %s", lr.Bookings, b.ID)
	}

	if rec := post(t, mux, body); rec.Code != http.StatusConflict {
		t.Fatalf("интервал после восстановления: код %d, хотим %d; тело %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestRestoreNotFound(t *testing.T) {
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
			name: "бронь не отменена",
			id: func(t *testing.T, mux *http.ServeMux) string {
				return createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`).ID
			},
		},
		{
			name: "повторное восстановление",
			id: func(t *testing.T, mux *http.ServeMux) string {
				b := cancelBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)
				if rec := restore(t, mux, b.ID); rec.Code != http.StatusOK {
					t.Fatalf("первое восстановление: код %d, тело %s", rec.Code, rec.Body.String())
				}
				return b.ID
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newServer()
			rec := restore(t, mux, tt.id(t, mux))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusNotFound, rec.Body.String())
			}
			assertErrorBody(t, rec)
		})
	}
}

func TestRestoreWindowExpired(t *testing.T) {
	mux, store := newServerWithStore()
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	b := cancelBooking(t, mux, `{"room":"green","start":"2026-08-24T12:00:00Z","end":"2026-08-24T13:00:00Z"}`)

	now = now.Add(10*time.Minute - time.Second)
	if rec := restore(t, mux, b.ID); rec.Code != http.StatusOK {
		t.Fatalf("внутри окна: код %d, хотим %d; тело %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec := del(t, mux, b.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("повторная отмена: код %d, тело %s", rec.Code, rec.Body.String())
	}

	now = now.Add(10*time.Minute + time.Second)
	rec := restore(t, mux, b.ID)
	if rec.Code != http.StatusGone {
		t.Fatalf("после окна: код %d, хотим %d; тело %s", rec.Code, http.StatusGone, rec.Body.String())
	}
	assertErrorBody(t, rec)

	list := get(t, mux, "room=green&date=2026-08-24")
	var lr listResponse
	if err := json.Unmarshal(list.Body.Bytes(), &lr); err != nil {
		t.Fatalf("разобрать список: %v", err)
	}
	if len(lr.Bookings) != 0 {
		t.Fatalf("после истечения окна броней = %d, хотим 0 (%s)", len(lr.Bookings), list.Body.String())
	}
}

func TestRestoreConflict(t *testing.T) {
	mux := newServer()
	b := cancelBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)
	createBooking(t, mux, `{"room":"green","start":"2026-08-24T10:30:00Z","end":"2026-08-24T11:30:00Z"}`)

	rec := restore(t, mux, b.ID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("код ответа = %d, хотим %d; тело %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	assertErrorBody(t, rec)
}

func TestRestoreConcurrent(t *testing.T) {
	mux := newServer()
	b := cancelBooking(t, mux, `{"room":"green","start":"2026-08-24T10:00:00Z","end":"2026-08-24T11:00:00Z"}`)

	const goroutines = 50
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		restored int
		other    []int
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			rec := restore(t, mux, b.ID)

			mu.Lock()
			defer mu.Unlock()
			switch rec.Code {
			case http.StatusOK:
				restored++
			case http.StatusNotFound:
			default:
				other = append(other, rec.Code)
			}
		}()
	}
	wg.Wait()

	if restored != 1 {
		t.Fatalf("успешных восстановлений = %d, хотим 1", restored)
	}
	if len(other) != 0 {
		t.Fatalf("неожидаемые коды ответов: %v", other)
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
