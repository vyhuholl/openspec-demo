package main

import (
	"log"
	"net/http"
	"os"

	"booking/internal/booking"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	booking.NewHandler(booking.NewStore()).Register(mux)

	addr := ":" + port
	log.Printf("слушаю %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("сервер остановлен: %v", err)
	}
}
