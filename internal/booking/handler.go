package booking

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type handler struct {
	store *Store
}

func NewHandler(s *Store) http.Handler {
	h := &handler{store: s}
	// Routing is manual: http.ServeMux answers unknown paths and methods with
	// text/plain and redirects paths like //bookings with text/html, while
	// every response of this service must be JSON.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bookings" {
			writeError(w, http.StatusNotFound, fmt.Sprintf("path %q not found", r.URL.Path))
			return
		}
		switch r.Method {
		case http.MethodPost:
			h.create(w, r)
		case http.MethodGet:
			h.list(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		}
	})
}

type bookingJSON struct {
	ID    string `json:"id"`
	Room  string `json:"room"`
	Start string `json:"start"`
	End   string `json:"end"`
}

func newBookingJSON(b Booking) bookingJSON {
	return bookingJSON{ID: b.ID, Room: b.Room, Start: formatTime(b.Start), End: formatTime(b.End)}
}

type listResponse struct {
	Bookings []bookingJSON `json:"bookings"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	room, start, end, err := parseCreateRequest(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	b, err := h.store.Create(room, start, end)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, newBookingJSON(b))
	case errors.Is(err, ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		log.Printf("create booking: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// parseCreateRequest looks fields up by exact key: encoding/json would match
// struct fields case-insensitively, so {"ROOM": ...} would count as room.
func parseCreateRequest(body io.Reader) (room string, start, end time.Time, err error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return "", time.Time{}, time.Time{}, fmt.Errorf("read request body: %w", err)
	}
	var fields map[string]json.RawMessage
	// Unlike json.Decoder, json.Unmarshal rejects data after the top-level value.
	if err := json.Unmarshal(data, &fields); err != nil {
		return "", time.Time{}, time.Time{}, fmt.Errorf("request body must be a JSON object: %w", err)
	}
	if room, err = stringField(fields, "room"); err != nil {
		return "", time.Time{}, time.Time{}, err
	}
	if start, err = timeField(fields, "start"); err != nil {
		return "", time.Time{}, time.Time{}, err
	}
	if end, err = timeField(fields, "end"); err != nil {
		return "", time.Time{}, time.Time{}, err
	}
	return room, start, end, nil
}

func stringField(fields map[string]json.RawMessage, key string) (string, error) {
	var s *string
	if raw, ok := fields[key]; ok {
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", fmt.Errorf("field %s must be a string: %w", key, err)
		}
	}
	if s == nil {
		return "", fmt.Errorf("field %s is required", key)
	}
	return *s, nil
}

func timeField(fields map[string]json.RawMessage, key string) (time.Time, error) {
	s, err := stringField(fields, key)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("field %s must be an RFC 3339 timestamp: %w", key, err)
	}
	if _, offset := t.Zone(); offset != 0 {
		return time.Time{}, fmt.Errorf("field %s must be in UTC: offset Z, +00:00 or -00:00", key)
	}
	return t, nil
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	room := q.Get("room")
	if isBlank(room) {
		writeError(w, http.StatusBadRequest, "query parameter room is required and must not be blank")
		return
	}
	from, err := time.Parse(time.DateOnly, q.Get("date"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "query parameter date must be a calendar date in YYYY-MM-DD format")
		return
	}

	bookings := h.store.ListDay(room, from, from.AddDate(0, 0, 1))
	res := listResponse{Bookings: make([]bookingJSON, 0, len(bookings))}
	for _, b := range bookings {
		res.Bookings = append(res.Bookings, newBookingJSON(b))
	}
	writeJSON(w, http.StatusOK, res)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status line is already sent, so an encoding error can only be logged.
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
