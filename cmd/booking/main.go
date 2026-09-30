package main

import (
	"cmp"
	"log"
	"net/http"
	"os"

	"booking/internal/booking"
)

func main() {
	srv := &http.Server{
		Addr:    ":" + cmp.Or(os.Getenv("PORT"), "8080"),
		Handler: booking.NewHandler(booking.NewStore()),
	}
	log.Printf("listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
