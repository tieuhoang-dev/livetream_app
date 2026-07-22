package models

import "time"

type Order struct {
	ID           string    `gorm:"type:char(36);primary_key" json:"id"`
	UserID       string    `gorm:"type:char(36);not null" json:"user_id"`
	LivestreamID *string   `gorm:"type:char(36)" json:"livestream_id"`
	TotalAmount  float64   `gorm:"type:decimal(10,2);not null" json:"total_amount"`
	Status       string    `gorm:"size:50;not null" json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	User       User       `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"user"`
	Livestream Livestream `gorm:"foreignKey:LivestreamID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"livestream"`
}
