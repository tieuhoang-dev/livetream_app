package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"livesteam_app/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := app.Run(ctx)
	stop()
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("API stopped: %v", err)
		os.Exit(1)
	}
}
