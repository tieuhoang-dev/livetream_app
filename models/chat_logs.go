package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ChatLog struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	SenderID    string             `bson:"sender_id" json:"sender_id"`
	ReceiverID  string             `bson:"livestream_id" json:"livestream_id"`
	MessageType string             `bson:"message_type" json:"message_type"`
	Content     string             `bson:"content" json:"content"`
	IsFlagged   bool               `bson:"is_flagged" json:"is_flagged"`
	FlagReason  string             `bson:"flag_reason,omitempty" json:"flag_reason,omitempty"`
	IsDeleted   bool               `bson:"is_deleted" json:"is_deleted"`
	SendAt   time.Time          `bson:"created_at" json:"created_at"`
	Status      string             `bson:"status" json:"status"`
}
