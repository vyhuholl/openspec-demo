package booking

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func bookingBody(room, start, end string) string {
	return fmt.Sprintf(`{"room":%q,"start":%q,"end":%q}`, room, start, end)
}

func serve(h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

func mustCreate(t *testing.T, h *Handler, room, start, end string) {
	t.Helper()
	rec := serve(h.Create, http.MethodPost, "/bookings", bookingBody(room, start, end))
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed %s %s-%s: status %d, body %s", room, start, end, rec.Code, rec.Body)
	}
}

func checkErrorBody(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body, err)
	}
	if resp.Error == "" {
		t.Fatalf("error body %q has empty error field", rec.Body)
	}
}

func TestCreate(t *testing.T) {
	type slot struct{ room, start, end string }

	tests := []struct {
		name       string
		existing   []slot
		body       string
		wantStatus int
	}{
		{
			name:       "created",
			body:       bookingBody("green", "2026-08-24T10:00:00Z", "2026-08-24T11:00:00Z"),
			wantStatus: http.StatusCreated,
		},
		{
			name:       "conflict exact match",
			existing:   []slot{{"green", "2026-08-24T10:00:00Z", "2026-08-24T11:00:00Z"}},
			body:       bookingBody("green", "2026-08-24T10:00:00Z", "2026-08-24T11:00:00Z"),
			wantStatus: http.StatusConflict,
		},
		{
			name:       "conflict overlaps start of existing",
			existing:   []slot{{"green", "2026-08-24T10:00:00Z", "2026-08-24T11:00:00Z"}},
			body:       bookingBody("green", "2026-08-24T09:30:00Z", "2026-08-24T10:30:00Z"),
			wantStatus: http.StatusConflict,
		},
		{
			name:       "conflict overlaps end of existing",
			existing:   []slot{{"green", "2026-08-24T10:00:00Z", "2026-08-24T11:00:00Z"}},
			body:       bookingBody("green", "2026-08-24T10:30:00Z", "2026-08-24T11:30:00Z"),
			wantStatus: http.StatusConflict,
		},
		{
			name:       "conflict nested inside existing",
			existing:   []slot{{"green", "2026-08-24T10:00:00Z", "2026-08-24T12:00:00Z"}},
			body:       bookingBody("green", "2026-08-24T10:15:00Z", "2026-08-24T11:45:00Z"),
			wantStatus: http.StatusConflict,
		},
		{
			name:       "conflict encloses existing",
			existing:   []slot{{"green", "2026-08-24T10:15:00Z", "2026-08-24T10:45:00Z"}},
			body:       bookingBody("green", "2026-08-24T10:00:00Z", "2026-08-24T12:00:00Z"),
			wantStatus: http.StatusConflict,
		},
		{
			name:       "same time in another room",
			existing:   []slot{{"green", "2026-08-24T10:00:00Z", "2026-08-24T11:00:00Z"}},
			body:       bookingBody("blue", "2026-08-24T10:00:00Z", "2026-08-24T11:00:00Z"),
			wantStatus: http.StatusCreated,
		},
		{
			name:       "invalid json",
			body:       `{"room": "green", "start": `,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "wrong field type",
			body:       `{"room": 42, "start": "2026-08-24T10:00:00Z", "end": "2026-08-24T11:00:00Z"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown start format",
			body:       bookingBody("green", "2026-08-24 10:00", "2026-08-24T11:00:00Z"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown end format",
			body:       bookingBody("green", "2026-08-24T10:00:00Z", "24.08.2026 11:00"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing start",
			body:       `{"room": "green", "end": "2026-08-24T11:00:00Z"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty room",
			body:       bookingBody("", "2026-08-24T10:00:00Z", "2026-08-24T11:00:00Z"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "blank room",
			body:       bookingBody("   ", "2026-08-24T10:00:00Z", "2026-08-24T11:00:00Z"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing room",
			body:       `{"start": "2026-08-24T10:00:00Z", "end": "2026-08-24T11:00:00Z"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "end equals start",
			body:       bookingBody("green", "2026-08-24T10:00:00Z", "2026-08-24T10:00:00Z"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "end before start",
			body:       bookingBody("green", "2026-08-24T11:00:00Z", "2026-08-24T10:00:00Z"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "start not in utc",
			body:       bookingBody("green", "2026-08-24T13:00:00+03:00", "2026-08-24T11:00:00Z"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "end not in utc",
			body:       bookingBody("green", "2026-08-24T10:00:00Z", "2026-08-24T14:00:00+03:00"),
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(NewStore())
			for _, s := range tt.existing {
				mustCreate(t, h, s.room, s.start, s.end)
			}

			rec := serve(h.Create, http.MethodPost, "/bookings", tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus != http.StatusCreated {
				checkErrorBody(t, rec)
				return
			}

			var req, got struct {
				ID    string `json:"id"`
				Room  string `json:"room"`
				Start string `json:"start"`
				End   string `json:"end"`
			}
			if err := json.Unmarshal([]byte(tt.body), &req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response %q: %v", rec.Body, err)
			}
			if !idPattern.MatchString(got.ID) {
				t.Fatalf("id = %q, want 16 hex chars", got.ID)
			}
			if got.Room != req.Room || got.Start != req.Start || got.End != req.End {
				t.Fatalf("got %+v, want room=%s start=%s end=%s", got, req.Room, req.Start, req.End)
			}
		})
	}
}

func TestList(t *testing.T) {
	h := NewHandler(NewStore())
	mustCreate(t, h, "green", "2026-08-24T14:00:00Z", "2026-08-24T15:00:00Z")
	mustCreate(t, h, "green", "2026-08-24T09:00:00Z", "2026-08-24T10:00:00Z")
	mustCreate(t, h, "green", "2026-08-24T11:30:00Z", "2026-08-24T12:30:00Z")
	mustCreate(t, h, "green", "2026-08-23T23:00:00Z", "2026-08-24T01:00:00Z")
	mustCreate(t, h, "green", "2026-08-23T10:00:00Z", "2026-08-23T11:00:00Z")
	mustCreate(t, h, "green", "2026-08-25T10:00:00Z", "2026-08-25T11:00:00Z")
	mustCreate(t, h, "blue", "2026-08-24T10:00:00Z", "2026-08-24T11:00:00Z")

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantStarts []string
	}{
		{
			name:       "sorted by start within day",
			query:      "room=green&date=2026-08-24",
			wantStatus: http.StatusOK,
			wantStarts: []string{
				"2026-08-23T23:00:00Z",
				"2026-08-24T09:00:00Z",
				"2026-08-24T11:30:00Z",
				"2026-08-24T14:00:00Z",
			},
		},
		{
			name:       "previous day",
			query:      "room=green&date=2026-08-23",
			wantStatus: http.StatusOK,
			wantStarts: []string{"2026-08-23T10:00:00Z", "2026-08-23T23:00:00Z"},
		},
		{
			name:       "next day",
			query:      "room=green&date=2026-08-25",
			wantStatus: http.StatusOK,
			wantStarts: []string{"2026-08-25T10:00:00Z"},
		},
		{
			name:       "other room",
			query:      "room=blue&date=2026-08-24",
			wantStatus: http.StatusOK,
			wantStarts: []string{"2026-08-24T10:00:00Z"},
		},
		{
			name:       "no bookings",
			query:      "room=red&date=2026-08-24",
			wantStatus: http.StatusOK,
			wantStarts: []string{},
		},
		{
			name:       "missing room",
			query:      "date=2026-08-24",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty room",
			query:      "room=&date=2026-08-24",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing date",
			query:      "room=green",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unparsable date",
			query:      "room=green&date=24.08.2026",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "impossible date",
			query:      "room=green&date=2026-02-30",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(h.List, http.MethodGet, "/bookings?"+tt.query, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus != http.StatusOK {
				checkErrorBody(t, rec)
				return
			}

			var resp struct {
				Bookings []struct {
					Room  string `json:"room"`
					Start string `json:"start"`
				} `json:"bookings"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response %q: %v", rec.Body, err)
			}
			if resp.Bookings == nil {
				t.Fatalf("bookings is null, want array: %s", rec.Body)
			}
			if len(resp.Bookings) != len(tt.wantStarts) {
				t.Fatalf("got %d bookings, want %d: %s", len(resp.Bookings), len(tt.wantStarts), rec.Body)
			}
			q, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatalf("parse query: %v", err)
			}
			room := q.Get("room")
			for i, b := range resp.Bookings {
				if b.Room != room {
					t.Fatalf("bookings[%d].room = %q, want %q", i, b.Room, room)
				}
				if b.Start != tt.wantStarts[i] {
					t.Fatalf("bookings[%d].start = %s, want %s", i, b.Start, tt.wantStarts[i])
				}
			}
		})
	}
}

func TestCreateConcurrentOverlapping(t *testing.T) {
	const n = 50
	h := NewHandler(NewStore())
	base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	end := base.Add(2 * time.Hour).Format(time.RFC3339)

	statuses := make([]int, n)
	ready := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		body := bookingBody("green", base.Add(time.Duration(i)*time.Minute).Format(time.RFC3339), end)
		wg.Go(func() {
			<-ready
			statuses[i] = serve(h.Create, http.MethodPost, "/bookings", body).Code
		})
	}
	close(ready)
	wg.Wait()

	var created, conflicts int
	for i, code := range statuses {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("request %d: unexpected status %d", i, code)
		}
	}
	if created != 1 || conflicts != n-1 {
		t.Fatalf("created = %d, conflicts = %d, want 1 and %d", created, conflicts, n-1)
	}

	rec := serve(h.List, http.MethodGet, "/bookings?room=green&date=2026-08-24", "")
	var resp struct {
		Bookings []Booking `json:"bookings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list %q: %v", rec.Body, err)
	}
	if len(resp.Bookings) != 1 {
		t.Fatalf("stored %d bookings, want 1", len(resp.Bookings))
	}
}
