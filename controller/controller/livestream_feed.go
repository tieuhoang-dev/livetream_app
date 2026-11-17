package controllers

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"livesteam_app/controller/services"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/gorilla/websocket"
)

// Public list subscriptions have their own connections. They must not replace
// the authenticated per-user socket used for chat and live commands.
var liveFeed = struct {
	sync.Mutex
	listeners map[chan struct{}]struct{}
}{listeners: make(map[chan struct{}]struct{})}

func notifyLivestreamsChanged() {
	liveFeed.Lock()
	defer liveFeed.Unlock()
	for listener := range liveFeed.listeners {
		select {
		case listener <- struct{}{}:
		default: // Coalesce changes while a subscriber is reading its next page.
		}
	}
}

type liveListRequest struct {
	Type      string                   `json:"type"`
	RequestID string                   `json:"request_id"`
	Query     services.LivestreamQuery `json:"query"`
}

// LivestreamFeed is read-only and exposes the same public fields as the live
// catalog. A valid list_lives request replaces this connection's subscription.
func LivestreamFeed(c *gin.Context) {
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	changed := make(chan struct{}, 1)
	liveFeed.Lock()
	liveFeed.listeners[changed] = struct{}{}
	liveFeed.Unlock()
	defer func() {
		liveFeed.Lock()
		delete(liveFeed.listeners, changed)
		liveFeed.Unlock()
	}()

	ws.SetReadLimit(16 << 10)
	ws.SetReadDeadline(time.Now().Add(70 * time.Second))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(70 * time.Second))
	})
	requests := make(chan json.RawMessage, 8)
	go func() {
		defer cancel()
		for {
			var raw json.RawMessage
			if err := ws.ReadJSON(&raw); err != nil {
				return
			}
			select {
			case requests <- raw:
			case <-ctx.Done():
				return
			}
		}
	}()
	write := func(payload interface{}) error {
		ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return ws.WriteJSON(payload)
	}
	var subscription *liveListRequest
	snapshot := func() error {
		items, total, err := services.ListLivestreams(subscription.Query)
		if err != nil {
			return write(gin.H{"type": "list_lives_error", "request_id": subscription.RequestID, "error": "Internal server error"})
		}
		return write(gin.H{
			"type": "list_lives_success", "request_id": subscription.RequestID,
			"data": items, "total": total, "page": subscription.Query.Page, "page_size": subscription.Query.PageSize,
		})
	}
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case raw := <-requests:
			request := liveListRequest{Query: services.LivestreamQuery{Pagination: defaultPagination(), Status: "start", Sort: "newest"}}
			if err := json.Unmarshal(raw, &request); err != nil || request.Type != "list_lives" || len(request.RequestID) > 64 || binding.Validator.ValidateStruct(request.Query) != nil {
				if err := write(gin.H{"type": "list_lives_error", "request_id": request.RequestID, "error": "invalid_query"}); err != nil {
					return
				}
				continue
			}
			subscription = &request
			if err := snapshot(); err != nil {
				return
			}
		case <-changed:
			if subscription != nil {
				if err := snapshot(); err != nil {
					return
				}
			}
		case <-ping.C:
			ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
