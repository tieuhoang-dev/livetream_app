package models

import "time"

type LivestreamComment struct {
	ID           uint64    `gorm:"primaryKey;index:idx_live_comment_cursor,priority:2" json:"id,string"`
	LivestreamID string    `gorm:"type:char(36);not null;index:idx_live_comment_cursor,priority:1" json:"-"`
	UserID       string    `gorm:"type:char(36);not null" json:"-"`
	Username     string    `gorm:"size:255;not null" json:"username"`
	Content      string    `gorm:"size:300;not null" json:"content"`
	CreatedAt    time.Time `json:"created_at"`
}
