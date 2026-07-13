package models

import "time"

type Transaction struct {
	ID        string    `gorm:"type:char(36);primary_key" json:"id"`
	WalletID  string    `gorm:"type:char(36);not null" json:"wallet_id"`
	Amount    float64   `gorm:"type:decimal(10,2);not null" json:"amount"`
	Status    string    `gorm:"size:50;not null" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Wallet    Wallet    `gorm:"foreignKey:WalletID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"wallet"`
}
