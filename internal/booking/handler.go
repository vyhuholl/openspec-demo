package booking

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

type createRequest struct {
	Room  string `json:"room"`
	Start string `json:"start"`
	End   string `json:"end"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read request body")
		return
	}
	var req createRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Room) == "" {
		writeError(w, http.StatusBadRequest, "room is required")
		return
	}
	start, err := parseUTC(req.Start)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("start: %v", err))
		return
	}
	end, err := parseUTC(req.End)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("end: %v", err))
		return
	}
	if !end.After(start) {
		writeError(w, http.StatusBadRequest, "end must be after start")
		return
	}

	b, err := h.store.Create(req.Room, start, end)
	if errors.Is(err, ErrConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	room := q.Get("room")
	if strings.TrimSpace(room) == "" {
		writeError(w, http.StatusBadRequest, "room is required")
		return
	}
	day, err := time.Parse(time.DateOnly, q.Get("date"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "date must be in YYYY-MM-DD format")
		return
	}

	bookings := h.store.List(room, day, day.AddDate(0, 0, 1))
	writeJSON(w, http.StatusOK, map[string][]Booking{"bookings": bookings})
}

func parseUTC(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("must be RFC3339, got %q", s)
	}
	if _, offset := t.Zone(); offset != 0 {
		return time.Time{}, fmt.Errorf("must be in UTC, got %q", s)
	}
	return t.UTC(), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
