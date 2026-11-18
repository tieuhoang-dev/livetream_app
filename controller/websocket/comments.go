package websocket

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"
	"livesteam_app/controller/services"
	"livesteam_app/internal/realtime"
)

func registerComments(hub *realtime.Service) {
	register(hub, "comments.subscribe", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ID    string `json:"livestream_id" binding:"required,uuid"`
			After string `json:"after"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		cursor := uint64(0)
		var err error
		if input.After != "" {
			cursor, err = strconv.ParseUint(input.After, 10, 64)
			if err != nil {
				return nil, realtime.Fail("invalid_pagination", 400)
			}
		}
		// Register before reading Mongo. Pushes during history loading are merged by ID.
		load := snapshot(func(ctx context.Context) (realtime.Snapshot, error) {
			rows, more, err := services.ListLivestreamComments(ctx, input.ID, cursor)
			if err != nil {
				return realtime.Snapshot{}, err
			}
			if len(rows) > 0 {
				cursor = rows[len(rows)-1].ID
			}
			return realtime.Snapshot{Data: gin.H{"data": rows, "next_cursor": strconv.FormatUint(cursor, 10), "has_more": more}, More: more}, nil
		})
		return gin.H{"subscribed": true}, c.Subscribe(req.SubscriptionID, "comments:"+input.ID, load, false)
	})
	register(hub, "comments.send", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ID      string `json:"livestream_id" binding:"required,uuid"`
			Content string `json:"content"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		identity := c.Identity()
		return services.AddLivestreamComment(ctx, input.ID, identity.ID, identity.Username, input.Content)
	})
}
