package models

import "time"

// DeliveryAddress is copied into orders, not joined to mutable profile data.
type DeliveryAddress struct {
	LocationMode string   `json:"location_mode,omitempty" gorm:"size:20"`
	Name         string   `json:"name" gorm:"size:100"`
	Phone        string   `json:"phone" gorm:"size:20"`
	ProvinceCode string   `json:"province_code" gorm:"size:8"`
	ProvinceName string   `json:"province_name" gorm:"size:100"`
	Ward         string   `json:"ward" gorm:"size:100"`
	AddressLine  string   `json:"address_line" gorm:"size:255"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
}

// A live can originate somewhere other than the seller's saved shop address.
// Never include this address in public livestream views.
type LivestreamPickup struct {
	LivestreamID string          `json:"-" gorm:"type:char(36);primaryKey"`
	Address      DeliveryAddress `json:"pickup_address" gorm:"embedded;embeddedPrefix:pickup_"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type ShippingSettings struct {
	SellerID         string          `json:"-" gorm:"type:char(36);primaryKey"`
	Pickup           DeliveryAddress `json:"pickup_address" gorm:"embedded;embeddedPrefix:pickup_"`
	SameProvinceFee  float64         `json:"same_province_fee" gorm:"type:decimal(15,2);not null"`
	OtherProvinceFee float64         `json:"other_province_fee" gorm:"type:decimal(15,2);not null"`
	FreeShippingFrom *float64        `json:"free_shipping_from" gorm:"type:decimal(15,2)"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type OrderDelivery struct {
	OrderID   string          `json:"-" gorm:"type:char(36);primaryKey"`
	Recipient DeliveryAddress `json:"recipient" gorm:"embedded;embeddedPrefix:recipient_"`
	Pickup    DeliveryAddress `json:"pickup" gorm:"embedded;embeddedPrefix:pickup_"`
	Note      string          `json:"note" gorm:"size:500"`
}
