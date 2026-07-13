package models

import "time"

type OrderItem struct {
	ID              string    `gorm:"type:char(36);primary_key" json:"id"`
	OrderID         string    `gorm:"type:char(36);not null" json:"order_id"`
	ProductID       string    `gorm:"type:char(36);not null" json:"product_id"`
	Quantity        int       `gorm:"type:int;not null" json:"quantity"`
	PriceAtPurchase float64   `gorm:"type:decimal(10,2);not null" json:"price_at_purchase"`
	CreatedAt       time.Time `json:"created_at"`
	Order           Order     `gorm:"foreignKey:OrderID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"order"`
	Product         Product   `gorm:"foreignKey:ProductID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"product"`
}
