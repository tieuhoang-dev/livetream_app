package models

type LivestreamProduct struct {
	ID            string  `gorm:"type:char(36);primary_key" json:"id"`
	LivestreamID  string  `gorm:"type:char(36);not null" json:"livestream_id"`
	ProductID     string  `gorm:"type:char(36);not null" json:"product_id"`
	DiscountPrice float64 `gorm:"type:decimal(10,2);not null" json:"discount_price"`
	IsPinned      bool    `gorm:"type:boolean;not null" json:"is_pinned"`

	Livestream Livestream `gorm:"foreignKey:LivestreamID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"livestream"`
	Product    Product    `gorm:"foreignKey:ProductID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"product"`
}
