package app

import (
	"context"
	"sync"

	"livesteam_app/controller/services"
)

func startWorkers() (func(), error) {
	ctx, cancel := context.WithCancel(context.Background())
	stopChat, err := services.StartDirectChatWorkers(ctx, 50)
	if err != nil {
		cancel()
		return nil, err
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); services.RunLivestreamLifecycle(ctx) }()
	return func() { cancel(); stopChat(); wg.Wait() }, nil
}
