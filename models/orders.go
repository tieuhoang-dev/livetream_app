package models

import "time"

type Order struct {
	ID            string    `gorm:"type:char(36);primary_key" json:"id"`
	UserID        string    `gorm:"type:char(36);not null;uniqueIndex:idx_order_request,priority:1" json:"user_id"`
	LivestreamID  *string   `gorm:"type:char(36)" json:"livestream_id"`
	TotalAmount   float64   `gorm:"type:decimal(15,2);not null" json:"total_amount"`
	ItemsSubtotal *float64  `gorm:"type:decimal(15,2)" json:"items_subtotal"`
	ShippingFee   *float64  `gorm:"type:decimal(15,2)" json:"shipping_fee"`
	RequestKey    *string   `gorm:"size:64;uniqueIndex:idx_order_request,priority:2" json:"-"`
	RequestHash   string    `gorm:"size:64" json:"-"`
	Status        string    `gorm:"size:50;not null" json:"status"`
	RefundReason  string    `gorm:"size:255" json:"refund_reason"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	User       User       `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"user"`
	Livestream Livestream `gorm:"foreignKey:LivestreamID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"livestream"`
}
