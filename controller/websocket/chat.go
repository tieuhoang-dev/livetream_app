package websocket

import (
	"context"
	"github.com/gin-gonic/gin"
	"livesteam_app/controller/services"
	"livesteam_app/internal/realtime"
)

func registerChat(hub *realtime.Service) {
	register(hub, "chat.subscribe", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		return gin.H{"subscribed": true}, c.Subscribe(req.SubscriptionID, "messages:"+c.Identity().ID, nil, false)
	})
	register(hub, "chat.send", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ReceiverID string `json:"receiver_id" binding:"required,uuid"`
			Type       string `json:"message_type"`
			Content    string `json:"content"`
			TempID     string `json:"temp_id"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		id := c.Identity()
		return services.QueueDirectChat(ctx, id.ID, id.Username, id.Avatar, input.ReceiverID, input.Type, input.Content, input.TempID)
	})
}
