package models

import "time"

type Product struct {
	ID              string    `gorm:"type:char(36);primary_key" json:"id"`
	SellerID        string    `gorm:"type:char(36);not null" json:"seller_id"`
	Name            string    `gorm:"size:255;not null" json:"name"`
	Price           float64   `gorm:"type:decimal(10,2);not null" json:"price"`
	PriceAtPurchase float64   `gorm:"type:decimal(10,2);not null" json:"price_at_purchase"`
	ImageURL        string    `gorm:"size:255;not null" json:"image_url"`
	StockQuantity   int       `gorm:"type:int;not null" json:"stock_quantity"`
	Description     string    `gorm:"size:255;not null" json:"description"`
	CreatedAt       time.Time `json:"created_at"`

	User User `gorm:"foreignKey:SellerID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"user"`
}
