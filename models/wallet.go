package models

import "time"

type Wallet struct {
	ID        string    `gorm:"type:char(36);primary_key" json:"id"`
	UserID    string    `gorm:"type:char(36);not null" json:"user_id"`
	Balance   float64   `gorm:"type:decimal(10,2);not null" json:"balance"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      User      `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"user"`
}
