package services

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"livesteam_app/config"
	"livesteam_app/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed provinces.json
var provinceData []byte
var provinceNames = func() map[string]string {
	var rows []struct{ Code, Name string }
	if err := json.Unmarshal(provinceData, &rows); err != nil {
		panic(err)
	}
	result := make(map[string]string, len(rows))
	for _, row := range rows {
		result[row.Code] = row.Name
	}
	return result
}()
var deliveryPhone = regexp.MustCompile(`^(0[0-9]{9,10}|\+84[0-9]{9,10})$`)

type OrderInput struct {
	ProductID     string                 `json:"product_id"`
	Quantity      int                    `json:"quantity"`
	LivestreamID  *string                `json:"livestream_id"`
	PaymentMethod string                 `json:"payment_method"`
	Delivery      models.DeliveryAddress `json:"delivery"`
	Note          string                 `json:"note"`
	RequestID     string                 `json:"request_id"`
	ExpectedTotal *float64               `json:"expected_total"`
}
type CheckoutQuote struct {
	UnitPrice      float64 `json:"unit_price"`
	Quantity       int     `json:"quantity"`
	ItemsSubtotal  float64 `json:"items_subtotal"`
	ShippingFee    float64 `json:"shipping_fee"`
	TotalAmount    float64 `json:"total_amount"`
	Currency       string  `json:"currency"`
	ShippingZone   string  `json:"shipping_zone"`
	PickupProvince string  `json:"pickup_province"`
}

func normalizeDelivery(input models.DeliveryAddress) (models.DeliveryAddress, error) {
	if input.LocationMode != "" && input.LocationMode != "manual" && input.LocationMode != "current" {
		return input, errors.New("invalid_delivery_address")
	}
	if input.LocationMode == "current" && (input.Latitude == nil || input.Longitude == nil) {
		return input, errors.New("invalid_delivery_address")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Phone = strings.TrimSpace(input.Phone)
	input.Phone = strings.NewReplacer(" ", "", "-", "", ".", "").Replace(input.Phone)
	input.Ward = strings.TrimSpace(input.Ward)
	input.AddressLine = strings.TrimSpace(input.AddressLine)
	code, err := strconv.Atoi(input.ProvinceCode)
	if err == nil {
		input.ProvinceCode = strconv.Itoa(code)
	}
	input.ProvinceName = provinceNames[input.ProvinceCode]
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 100 || !deliveryPhone.MatchString(input.Phone) || input.ProvinceName == "" || input.Ward == "" || utf8.RuneCountInString(input.Ward) > 100 || input.AddressLine == "" || utf8.RuneCountInString(input.AddressLine) > 255 {
		return input, errors.New("invalid_delivery_address")
	}
	if (input.Latitude == nil) != (input.Longitude == nil) {
		return input, errors.New("invalid_delivery_address")
	}
	if input.Latitude != nil {
		lat, lng := *input.Latitude, *input.Longitude
		if math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lng) || math.IsInf(lng, 0) || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
			return input, errors.New("invalid_delivery_address")
		}
	}
	return input, nil
}
func moneyCents(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1e12 {
		return 0, errors.New("invalid_price")
	}
	return int64(math.Round(value * 100)), nil
}
func normalizeOrderInput(input OrderInput) (OrderInput, string, error) {
	if input.Quantity <= 0 || input.Quantity > 1000000 {
		return input, "", errors.New("invalid_quantity")
	}
	if _, err := uuid.Parse(input.ProductID); err != nil {
		return input, "", errors.New("invalid_checkout_request")
	}
	if input.LivestreamID != nil {
		if _, err := uuid.Parse(*input.LivestreamID); err != nil {
			return input, "", errors.New("invalid_checkout_request")
		}
	}
	input.PaymentMethod = strings.TrimSpace(input.PaymentMethod)
	if input.PaymentMethod != "cod" && input.PaymentMethod != "bank_transfer" && input.PaymentMethod != "manual" {
		return input, "", errors.New("invalid_payment_method")
	}
	address, err := normalizeDelivery(input.Delivery)
	if err != nil {
		return input, "", err
	}
	input.Delivery = address
	input.Note = strings.TrimSpace(input.Note)
	if utf8.RuneCountInString(input.Note) > 500 {
		return input, "", errors.New("invalid_checkout_request")
	}
	if input.RequestID != "" {
		id, err := uuid.Parse(input.RequestID)
		if err != nil {
			return input, "", errors.New("invalid_checkout_request")
		}
		input.RequestID = id.String()
	}
	if input.ExpectedTotal != nil {
		if _, err := moneyCents(*input.ExpectedTotal); err != nil {
			return input, "", errors.New("invalid_checkout_request")
		}
	}
	fingerprint := input
	fingerprint.ExpectedTotal = nil
	fingerprint.RequestID = ""
	body, _ := json.Marshal(fingerprint)
	digest := sha256.Sum256(body)
	return input, hex.EncodeToString(digest[:]), nil
}

type purchaseInfo struct {
	Price    float64
	Stock    int
	SellerID string
	Target   interface{}
}

func checkoutProduct(db *gorm.DB, userID string, input OrderInput, lock bool) (purchaseInfo, error) {
	query := func() *gorm.DB {
		if lock {
			return db.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		return db.Session(&gorm.Session{})
	}
	var info purchaseInfo
	if input.LivestreamID != nil {
		var live models.Livestream
		if err := query().Take(&live, "id = ?", *input.LivestreamID).Error; err != nil {
			return info, lookupError(err, "livestream_not_found")
		}
		if live.Status != "start" {
			return info, errors.New("livestream_not_active")
		}
		var p models.LivestreamProduct
		if err := query().Where("livestream_id = ? AND product_id = ?", *input.LivestreamID, input.ProductID).Take(&p).Error; err != nil {
			return info, lookupError(err, "product_not_found_in_livestream")
		}
		info = purchaseInfo{Price: p.DiscountPrice, Stock: p.StockQuantity, SellerID: live.SellerID, Target: &p}
	} else {
		var p models.Product
		if err := query().Take(&p, "id = ?", input.ProductID).Error; err != nil {
			return info, lookupError(err, "product_not_found")
		}
		info = purchaseInfo{Price: p.Price, Stock: p.StockQuantity, SellerID: p.SellerID, Target: &p}
	}
	if userID == info.SellerID {
		return info, errors.New("cannot_buy_own_product")
	}
	if info.Stock < input.Quantity {
		return info, errors.New("insufficient_stock")
	}
	return info, nil
}
func shippingPolicy(db *gorm.DB, sellerID string, lock bool) (models.ShippingSettings, error) {
	var settings models.ShippingSettings
	if lock {
		db = db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := db.Take(&settings, "seller_id = ?", sellerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return settings, errors.New("shipping_not_configured")
		}
		return settings, err
	}
	return settings, nil
}
func checkoutShippingPolicy(db *gorm.DB, input OrderInput, sellerID string, lock bool) (models.ShippingSettings, error) {
	settings, err := shippingPolicy(db, sellerID, lock)
	if err != nil || input.LivestreamID == nil {
		return settings, err
	}
	query := db
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var pickup models.LivestreamPickup
	err = query.Take(&pickup, "livestream_id = ?", *input.LivestreamID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return settings, nil // Older lives retain their saved shop origin.
	}
	if err != nil {
		return settings, err
	}
	settings.Pickup = pickup.Address
	return settings, nil
}
func priceCheckout(info purchaseInfo, input OrderInput, settings models.ShippingSettings) (CheckoutQuote, error) {
	unit, err := moneyCents(info.Price)
	if err != nil {
		return CheckoutQuote{}, err
	}
	if unit > 100000000000000/int64(input.Quantity) {
		return CheckoutQuote{}, errors.New("invalid_price")
	}
	subtotal := unit * int64(input.Quantity)
	fee, zone := settings.OtherProvinceFee, "other_province"
	if input.Delivery.ProvinceCode == settings.Pickup.ProvinceCode {
		fee, zone = settings.SameProvinceFee, "same_province"
	}
	feeCents, err := moneyCents(fee)
	if err != nil {
		return CheckoutQuote{}, err
	}
	if settings.FreeShippingFrom != nil {
		threshold, err := moneyCents(*settings.FreeShippingFrom)
		if err != nil {
			return CheckoutQuote{}, err
		}
		if subtotal >= threshold {
			feeCents, zone = 0, "free"
		}
	}
	if subtotal+feeCents > 100000000000000 {
		return CheckoutQuote{}, errors.New("invalid_price")
	}
	return CheckoutQuote{UnitPrice: float64(unit) / 100, Quantity: input.Quantity, ItemsSubtotal: float64(subtotal) / 100, ShippingFee: float64(feeCents) / 100, TotalAmount: float64(subtotal+feeCents) / 100, Currency: "VND", ShippingZone: zone, PickupProvince: settings.Pickup.ProvinceName}, nil
}
func QuoteOrder(ctx context.Context, userID string, input OrderInput) (CheckoutQuote, error) {
	input, _, err := normalizeOrderInput(input)
	if err != nil {
		return CheckoutQuote{}, err
	}
	db := config.DB.WithContext(ctx)
	info, err := checkoutProduct(db, userID, input, false)
	if err != nil {
		return CheckoutQuote{}, err
	}
	settings, err := checkoutShippingPolicy(db, input, info.SellerID, false)
	if err != nil {
		return CheckoutQuote{}, err
	}
	return priceCheckout(info, input, settings)
}
func replayOrder(db *gorm.DB, userID, key, hash string) (*models.Order, error) {
	if key == "" {
		return nil, nil
	}
	var order models.Order
	err := db.Where("user_id = ? AND request_key = ?", userID, key).Take(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if order.RequestHash != hash {
		return nil, errors.New("duplicate_request_conflict")
	}
	return &order, nil
}
func GetShippingSettings(ctx context.Context, sellerID string) (*models.ShippingSettings, error) {
	var row models.ShippingSettings
	err := config.DB.WithContext(ctx).Take(&row, "seller_id = ?", sellerID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}
func SaveShippingSettings(ctx context.Context, sellerID string, row models.ShippingSettings) (*models.ShippingSettings, error) {
	address, err := normalizeDelivery(row.Pickup)
	if err != nil {
		return nil, err
	}
	row.Pickup = address
	row.SellerID = sellerID
	for _, fee := range []float64{row.SameProvinceFee, row.OtherProvinceFee} {
		if _, err := moneyCents(fee); err != nil {
			return nil, errors.New("invalid_shipping_settings")
		}
	}
	if row.FreeShippingFrom != nil {
		if value, err := moneyCents(*row.FreeShippingFrom); err != nil || value <= 0 {
			return nil, errors.New("invalid_shipping_settings")
		}
	}
	if err := config.DB.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
