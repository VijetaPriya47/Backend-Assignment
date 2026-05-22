package main

import (
	"log"
	"net/http"
	"os"

	"github.com/source-asia-backend/server"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Println("starting server on", addr)
	if err := http.ListenAndServe(addr, server.New()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
