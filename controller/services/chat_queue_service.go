package services

import (
	"context"
	"livesteam_app/config"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type QueueChatInput struct {
	MessageID  string
	TempID     string
	SenderID   uuid.UUID
	ReceiverID uuid.UUID
	Type       string
	Content    string
	CreatedAt  time.Time
}

func PushChatMessage(input QueueChatInput) error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		2*time.Second,
	)
	defer cancel()

	_, err := config.RedisClient.XAdd(
		ctx,
		&redis.XAddArgs{
			Stream: ChatStream,

			Values: map[string]interface{}{
				"id":          input.MessageID,
				"temp_id":     input.TempID,
				"sender_id":   input.SenderID.String(),
				"receiver_id": input.ReceiverID.String(),
				"type":        input.Type,
				"content":     input.Content,
				"created_at":  input.CreatedAt.UnixMilli(),
			},
		},
	).Result()

	return err
}
