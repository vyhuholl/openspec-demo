package booking

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

const dateLayout = "2006-01-02"

type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /bookings", h.create)
	mux.HandleFunc("GET /bookings", h.list)
	mux.HandleFunc("DELETE /bookings/{id}", h.cancel)
	mux.HandleFunc("POST /bookings/{id}/restore", h.restore)
}

type createRequest struct {
	Room  string `json:"room"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type listResponse struct {
	Bookings []Booking `json:"bookings"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "невалидный JSON")
		return
	}

	if req.Room == "" {
		writeError(w, http.StatusBadRequest, "поле room обязательно")
		return
	}

	start, err := parseUTC(req.Start)
	if err != nil {
		writeError(w, http.StatusBadRequest, "поле start: "+err.Error())
		return
	}
	end, err := parseUTC(req.End)
	if err != nil {
		writeError(w, http.StatusBadRequest, "поле end: "+err.Error())
		return
	}
	if !start.Before(end) {
		writeError(w, http.StatusBadRequest, "end должен быть больше start")
		return
	}

	b, err := h.store.Create(req.Room, start, end)
	if errors.Is(err, ErrConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "внутренняя ошибка")
		return
	}

	writeJSON(w, http.StatusCreated, b)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if room == "" {
		writeError(w, http.StatusBadRequest, "параметр room обязателен")
		return
	}

	raw := r.URL.Query().Get("date")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "параметр date обязателен")
		return
	}
	date, err := time.ParseInLocation(dateLayout, raw, time.UTC)
	if err != nil {
		writeError(w, http.StatusBadRequest, "параметр date: ожидается YYYY-MM-DD")
		return
	}

	writeJSON(w, http.StatusOK, listResponse{Bookings: h.store.ListByDate(room, date)})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	err := h.store.Delete(r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "внутренняя ошибка")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	b, err := h.store.Restore(r.PathValue("id"))
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrNotCancelled):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrRestoreExpired):
		writeError(w, http.StatusGone, err.Error())
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "внутренняя ошибка")
	default:
		writeJSON(w, http.StatusOK, b)
	}
}

func parseUTC(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, errors.New("обязательно")
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, errors.New("ожидается время в формате RFC3339")
	}
	if _, offset := t.Zone(); offset != 0 {
		return time.Time{}, errors.New("время должно быть в UTC")
	}
	return t.UTC(), nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
