package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"booking/internal/booking"
)

const defaultRestoreWindow = 10 * time.Minute

func parseRestoreWindow(raw string) (time.Duration, error) {
	if raw == "" {
		return defaultRestoreWindow, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("RESTORE_WINDOW: %w", err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("RESTORE_WINDOW: окно должно быть положительным, получили %s", d)
	}
	return d, nil
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	restoreWindow, err := parseRestoreWindow(os.Getenv("RESTORE_WINDOW"))
	if err != nil {
		log.Fatalf("%v", err)
	}

	mux := http.NewServeMux()
	booking.NewHandler(booking.NewStore(time.Now, restoreWindow)).Register(mux)

	addr := ":" + port
	log.Printf("слушаю %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("сервер остановлен: %v", err)
	}
}
