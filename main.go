package main

import (
	"log"

	"loopserve/internal/server"
)

func main() {
	srv, err := server.New(8081)
	if err != nil {
		log.Fatal("Failed to create server:", err)
	}

	if err := srv.Start(); err != nil {
		log.Fatal("Server failed:", err)
	}
}
