package main

import (
	"log"
	"net/http"
	"os"

	"openspec-demo/internal/booking"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	h := booking.NewHandler(booking.NewStore())
	mux := http.NewServeMux()
	mux.HandleFunc("POST /bookings", h.Create)
	mux.HandleFunc("GET /bookings", h.List)

	log.Fatal(http.ListenAndServe(":"+port, mux))
}
