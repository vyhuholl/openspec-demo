package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"booking/internal/booking"
)

const defaultRestoreWindow = 10 * time.Minute

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	restoreWindow := defaultRestoreWindow
	if raw := os.Getenv("RESTORE_WINDOW"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("RESTORE_WINDOW: %v", err)
		}
		restoreWindow = d
	}

	mux := http.NewServeMux()
	booking.NewHandler(booking.NewStore(restoreWindow)).Register(mux)

	addr := ":" + port
	log.Printf("слушаю %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("сервер остановлен: %v", err)
	}
}
