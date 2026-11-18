// Package app assembles storage, transports and workers and owns their lifetime.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"livesteam_app/config"
)

func Run(ctx context.Context) (result error) {
	settings, err := loadSettings()
	if err != nil {
		return err
	}
	resources, err := openStorage(ctx)
	if err != nil {
		return err
	}
	// Run only accepts traffic after schema/index preparation and worker startup.
	var stopWorkers func()
	var closeRealtime func()
	defer func() {
		if closeRealtime != nil {
			closeRealtime()
		}
		if stopWorkers != nil {
			stopWorkers()
		}
		result = errors.Join(result, resources.close())
	}()
	router, err := newRouter()
	if err != nil {
		return err
	}
	closeRealtime = config.Realtime.Close
	listener, err := net.Listen("tcp", settings.address)
	if err != nil {
		return fmt.Errorf("listen %s: %w", settings.address, err)
	}
	defer listener.Close()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	stopWorkers, err = startWorkers()
	if err != nil {
		return err
	}
	server := &http.Server{Addr: settings.address, Handler: router, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("API listening on %s", settings.address)
	return serve(ctx, server, listener)
}
