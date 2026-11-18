package services

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"livesteam_app/config"
	"livesteam_app/models"
)

// QueueDirectChat is for private messages only. Live comments use their own
// synchronous Mongo write and room topic; they never enter the private queue.
func QueueDirectChat(ctx context.Context, senderID, username, avatar, receiverID, kind, content, tempID string) (any, error) {
	receiver, err := uuid.Parse(receiverID)
	if err != nil || receiver == uuid.Nil {
		return nil, errors.New("invalid_chat_message")
	}
	content = strings.TrimSpace(content)
	if content == "" || utf8.RuneCountInString(content) > 4000 || len(tempID) > 64 {
		return nil, errors.New("invalid_chat_message")
	}
	switch kind {
	case "text", "message", "image", "video", "voice", "file":
	default:
		return nil, errors.New("invalid_chat_message")
	}
	if config.RedisClient == nil {
		return nil, errors.New("chat_storage_unavailable")
	}
	var user models.User
	if err := config.DB.WithContext(ctx).Select("id").Take(&user, "id = ? AND status = ?", receiver.String(), "active").Error; err != nil {
		return nil, lookupError(err, "user_not_found")
	}
	id := uuid.NewString()
	if tempID == "" {
		tempID = uuid.NewString()
	}
	created := time.Now().UTC()
	if err := PushChatMessage(QueueChatInput{MessageID: id, TempID: tempID, SenderID: uuid.MustParse(senderID), ReceiverID: receiver, Type: kind, Content: content, CreatedAt: created}); err != nil {
		return nil, errors.New("chat_storage_unavailable")
	}
	message := map[string]any{"id": id, "temp_id": tempID, "from": senderID, "to": receiver.String(), "type": kind, "content": content, "username": username, "avatar": avatar, "created_at": created, "status": "queued"}
	config.Realtime.Publish("messages:"+senderID, "message.queued", message)
	if receiver.String() != senderID {
		config.Realtime.Publish("messages:"+receiver.String(), "message.queued", message)
	}
	return message, nil
}
