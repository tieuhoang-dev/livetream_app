package models

type LivestreamProduct struct {
	ID           string `gorm:"type:char(36);primary_key" json:"id"`
	LivestreamID string `gorm:"type:char(36);not null;index:idx_live_products_live_product,priority:1" json:"livestream_id"`
	// Keep the existing Go/JSON name while storing the reference in product_id.
	ProductCode     string  `gorm:"column:product_id;type:char(36);not null;index:idx_live_products_live_product,priority:2" json:"product_code"`
	DiscountPrice   float64 `gorm:"type:decimal(10,2);not null" json:"discount_price"`
	PriceAtPurchase float64 `gorm:"type:decimal(10,2);not null" json:"price_at_purchase"`
	ProductName     string  `gorm:"size:255;not null" json:"product_name"`
	ProductImage    string  `gorm:"size:255;not null" json:"product_image"`
	Description     string  `gorm:"size:255;not null" json:"description"`
	StockQuantity   int     `gorm:"type:int;not null" json:"stock_quantity"`
	IsPinned        bool    `gorm:"type:boolean;not null" json:"is_pinned"`
	Status          string  `gorm:"size:50;not null" json:"status"`

	Livestream Livestream `gorm:"foreignKey:LivestreamID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"livestream"`
}
