package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Shutdown drains ordinary HTTP requests. Upgraded WebSockets are owned and
// joined separately by the realtime root service before storage is closed.
func serve(ctx context.Context, server *http.Server, listener net.Listener) error {
	if ctx.Err() != nil {
		listener.Close()
		return ctx.Err()
	}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	var serveErr error
	completed := false
	select {
	case serveErr = <-finished:
		completed = true
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		server.Close()
	}
	if !completed {
		serveErr = <-finished
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	if shutdownErr != nil {
		shutdownErr = fmt.Errorf("drain HTTP server: %w", shutdownErr)
	}
	return errors.Join(serveErr, shutdownErr)
}
