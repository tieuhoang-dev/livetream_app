package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"livesteam_app/config"
	"livesteam_app/internal/shipping"
	"livesteam_app/models"

	"gorm.io/gorm"
)

func validProductParcel(p models.ProductParcel) bool {
	return p.WeightGrams > 0 && p.WeightGrams <= 1000000 && p.LengthCM > 0 && p.LengthCM <= 300 && p.WidthCM > 0 && p.WidthCM <= 300 && p.HeightCM > 0 && p.HeightCM <= 300
}

type ShippingProvince struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	ProvinceCode string `json:"province_code"`
}

func ShippingProvinces(ctx context.Context) ([]ShippingProvince, error) {
	provider, err := config.Shipping()
	if err != nil {
		return nil, err
	}
	rows, err := provider.Provinces(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ShippingProvince, 0, len(rows))
	for _, row := range rows {
		if code := reverseProvince(row.Name); code != "" {
			result = append(result, ShippingProvince{ID: row.ID, Name: row.Name, ProvinceCode: code})
		}
	}
	return result, nil
}

func carrierRoute(ctx context.Context, provider shipping.Provider, address models.DeliveryAddress, pickup bool) (shipping.Route, error) {
	if address.CarrierProvinceID <= 0 || address.CarrierDistrictID <= 0 || address.CarrierWardCode == "" {
		return shipping.Route{}, errors.New("shipping_address_required")
	}
	provinces, err := provider.Provinces(ctx)
	if err != nil {
		return shipping.Route{}, err
	}
	found := false
	for _, row := range provinces {
		if row.ID == address.CarrierProvinceID && reverseProvince(row.Name) == address.ProvinceCode {
			found = true
			break
		}
	}
	if !found {
		return shipping.Route{}, errors.New("shipping_address_unavailable")
	}
	districts, err := provider.Districts(ctx, address.CarrierProvinceID)
	if err != nil {
		return shipping.Route{}, err
	}
	supports := func(kind int) bool { return kind == 3 || (pickup && kind == 1) || (!pickup && kind == 2) }
	found = false
	for _, row := range districts {
		if row.ID == address.CarrierDistrictID && supports(row.SupportType) {
			found = true
			break
		}
	}
	if !found {
		return shipping.Route{}, errors.New("shipping_address_unavailable")
	}
	wards, err := provider.Wards(ctx, address.CarrierDistrictID)
	if err != nil {
		return shipping.Route{}, err
	}
	for _, ward := range wards {
		if ward.Code == address.CarrierWardCode && supports(ward.SupportType) {
			return shipping.Route{DistrictID: address.CarrierDistrictID, WardCode: ward.Code, WardName: ward.Name}, nil
		}
	}
	return shipping.Route{}, errors.New("shipping_address_unavailable")
}

type preparedCheckout struct {
	Info      purchaseInfo
	Pickup    models.DeliveryAddress
	Recipient models.DeliveryAddress
	Quote     CheckoutQuote
	Version   string
}

func checkoutVersion(info purchaseInfo, pickup models.DeliveryAddress) string {
	// Stock is checked again under lock; price, parcel and pickup must stay the
	// same between the external read-only quote and the local SQL transaction.
	body, _ := json.Marshal(struct {
		Price  float64
		Seller string
		Parcel models.ProductParcel
		Pickup models.DeliveryAddress
	}{info.Price, info.SellerID, info.Parcel, pickup})
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}
func prepareCheckout(ctx context.Context, db *gorm.DB, userID string, input OrderInput) (preparedCheckout, error) {
	var result preparedCheckout
	info, err := checkoutProduct(db, userID, input, false)
	if err != nil {
		return result, err
	}
	pickup, err := checkoutPickup(db, input, info.SellerID, false)
	if err != nil {
		return result, err
	}
	if !validProductParcel(info.Parcel) {
		return result, errors.New("shipping_product_incomplete")
	}
	if info.Parcel.WeightGrams > 1000000/input.Quantity || info.Parcel.HeightCM > 300/input.Quantity {
		return result, errors.New("shipping_invalid_parcel")
	}
	parcel := models.ProductParcel{WeightGrams: info.Parcel.WeightGrams * input.Quantity, LengthCM: info.Parcel.LengthCM, WidthCM: info.Parcel.WidthCM, HeightCM: info.Parcel.HeightCM * input.Quantity}
	unit, err := moneyCents(info.Price)
	if err != nil {
		return result, err
	}
	if unit > 100000000000000/int64(input.Quantity) {
		return result, errors.New("invalid_price")
	}
	subtotal := unit * int64(input.Quantity)
	provider, err := config.Shipping()
	if err != nil {
		return result, err
	}
	from, err := carrierRoute(ctx, provider, pickup, true)
	if err != nil {
		if err.Error() == "shipping_address_required" || err.Error() == "shipping_address_unavailable" {
			return result, errors.New("shipping_pickup_area_required")
		}
		return result, err
	}
	to, err := carrierRoute(ctx, provider, input.Delivery, false)
	if err != nil {
		return result, err
	}
	cod := int64(0)
	if input.PaymentMethod == "cod" {
		cod = (subtotal + 99) / 100
	}
	fee, err := provider.Quote(ctx, shipping.QuoteRequest{From: from, To: to, Parcel: shipping.Parcel{WeightGrams: parcel.WeightGrams, LengthCM: parcel.LengthCM, WidthCM: parcel.WidthCM, HeightCM: parcel.HeightCM}, DeclaredValue: (subtotal + 99) / 100, CODValue: cod})
	if err != nil {
		return result, err
	}
	if fee.Total < 0 || fee.Total > 1000000000000 || subtotal+fee.Total*100 > 100000000000000 {
		return result, errors.New("shipping_unavailable")
	}
	snapshot := &models.ShippingQuoteSnapshot{Provider: "ghn", Environment: provider.Environment(), QuoteOnly: true, ServiceTypeID: fee.ServiceTypeID, TotalFee: fee.Total, ServiceFee: fee.ServiceFee, InsuranceFee: fee.InsuranceFee, Parcel: parcel, QuotedAt: time.Now().UTC()}
	canonicalPickup, recipient := pickup, input.Delivery
	canonicalPickup.Ward, recipient.Ward = from.WardName, to.WardName
	result = preparedCheckout{Info: info, Pickup: canonicalPickup, Recipient: recipient, Version: checkoutVersion(info, pickup), Quote: CheckoutQuote{
		UnitPrice: float64(unit) / 100, Quantity: input.Quantity, ItemsSubtotal: float64(subtotal) / 100, ShippingFee: float64(fee.Total), TotalAmount: float64(subtotal)/100 + float64(fee.Total), Currency: "VND", ShippingZone: "carrier", PickupProvince: pickup.ProvinceName, Shipping: snapshot,
	}}
	return result, nil
}
