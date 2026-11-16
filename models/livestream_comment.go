package models

import "time"

type LivestreamComment struct {
	DocumentID   string    `bson:"_id" json:"-"`
	ID           uint64    `bson:"sequence" json:"id,string"`
	LivestreamID string    `bson:"livestream_id" json:"-"`
	UserID       string    `bson:"sender_id" json:"-"`
	Username     string    `bson:"username" json:"username"`
	Content      string    `bson:"content" json:"content"`
	CreatedAt    time.Time `bson:"created_at" json:"created_at"`
	MessageType  string    `bson:"message_type" json:"-"`
}
