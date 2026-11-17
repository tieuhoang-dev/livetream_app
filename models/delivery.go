package models

import "time"

// DeliveryAddress is copied into orders, not joined to mutable profile data.
type DeliveryAddress struct {
	StationID         int      `json:"station_id,omitempty"`
	StationName       string   `json:"station_name,omitempty" gorm:"size:255"`
	CarrierProvinceID int      `json:"carrier_province_id,omitempty"`
	CarrierDistrictID int      `json:"carrier_district_id,omitempty"`
	CarrierWardCode   string   `json:"carrier_ward_code,omitempty" gorm:"size:32"`
	LocationMode      string   `json:"location_mode,omitempty" gorm:"size:20"`
	Name              string   `json:"name" gorm:"size:100"`
	Phone             string   `json:"phone" gorm:"size:20"`
	ProvinceCode      string   `json:"province_code" gorm:"size:8"`
	ProvinceName      string   `json:"province_name" gorm:"size:100"`
	Ward              string   `json:"ward" gorm:"size:100"`
	AddressLine       string   `json:"address_line" gorm:"size:255"`
	Latitude          *float64 `json:"latitude"`
	Longitude         *float64 `json:"longitude"`
}

type ProductParcel struct {
	WeightGrams int `json:"weight_grams"`
	LengthCM    int `json:"length_cm"`
	WidthCM     int `json:"width_cm"`
	HeightCM    int `json:"height_cm"`
}
type ShippingQuoteSnapshot struct {
	OriginType    string        `json:"origin_type,omitempty" gorm:"size:24"`
	StationID     int           `json:"station_id,omitempty"`
	StationName   string        `json:"station_name,omitempty" gorm:"size:255"`
	Provider      string        `json:"provider" gorm:"size:16"`
	Environment   string        `json:"environment" gorm:"size:16"`
	QuoteOnly     bool          `json:"quote_only"`
	ServiceTypeID int           `json:"service_type_id"`
	TotalFee      int64         `json:"total_fee"`
	ServiceFee    int64         `json:"service_fee"`
	InsuranceFee  int64         `json:"insurance_fee"`
	Parcel        ProductParcel `json:"parcel" gorm:"embedded;embeddedPrefix:parcel_"`
	QuotedAt      time.Time     `json:"quoted_at"`
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
	SameProvinceFee  float64         `json:"-" gorm:"type:decimal(15,2);not null"`
	OtherProvinceFee float64         `json:"-" gorm:"type:decimal(15,2);not null"`
	FreeShippingFrom *float64        `json:"-" gorm:"type:decimal(15,2)"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type OrderDelivery struct {
	Shipping  *ShippingQuoteSnapshot `json:"shipping,omitempty" gorm:"embedded;embeddedPrefix:shipping_"`
	OrderID   string                 `json:"-" gorm:"type:char(36);primaryKey"`
	Recipient DeliveryAddress        `json:"recipient" gorm:"embedded;embeddedPrefix:recipient_"`
	Pickup    DeliveryAddress        `json:"pickup" gorm:"embedded;embeddedPrefix:pickup_"`
	Note      string                 `json:"note" gorm:"size:500"`
}
