package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ChatLog struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	SenderID     string             `bson:"sender_id" json:"sender_id"`
	LivestreamID string             `bson:"livestream_id" json:"livestream_id"`
	Content      string             `bson:"content" json:"content"`
	IsFlagged    bool               `bson:"is_flagged" json:"is_flagged"`
	FlagReason   string             `bson:"flag_reason,omitempty" json:"flag_reason,omitempty"`
	IsDeleted    bool               `bson:"is_deleted" json:"is_deleted"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
}