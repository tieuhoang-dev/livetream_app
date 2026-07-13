package models

import "time"

type Livestream struct {
	ID        string    `gorm:"type:char(36);primary_key" json:"id"`
	SellerID  string    `gorm:"type:char(36);not null" json:"seller_id"`
	Title     string    `gorm:"size:255;not null" json:"title"`
	Status    string    `gorm:"size:50;not null" json:"status"`
	StreamURL string    `gorm:"size:255;not null" json:"stream_url"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`

	User User `gorm:"foreignKey:SellerID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"user"`
}
