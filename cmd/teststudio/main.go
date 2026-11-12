package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"livesteam_app/teststudio"
)

func main() {
	port := os.Getenv("TEST_STUDIO_PORT")
	if port == "" {
		port = "4173"
	}
	address := "127.0.0.1:" + port
	fmt.Printf("Livestream Test Studio: http://%s\n", address)
	server := &http.Server{Addr: address, Handler: teststudio.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(server.ListenAndServe())
}
