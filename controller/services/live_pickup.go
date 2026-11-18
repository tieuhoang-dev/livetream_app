package services

import (
	"context"
	"errors"

	"livesteam_app/config"
	"livesteam_app/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func GetLivePickup(ctx context.Context, userID, liveID string) (*models.DeliveryAddress, error) {
	db := config.DB.WithContext(ctx)
	var live models.Livestream
	if err := db.Take(&live, "id = ?", liveID).Error; err != nil {
		return nil, lookupError(err, "livestream_not_found")
	}
	if live.SellerID != userID {
		return nil, errors.New("forbidden")
	}
	var pickup models.LivestreamPickup
	err := db.Take(&pickup, "livestream_id = ?", liveID).Error
	if err == nil {
		return &pickup.Address, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	settings, err := GetShippingSettings(ctx, userID)
	if err != nil || settings == nil {
		return nil, err
	}
	return &settings.Pickup, nil
}

func SaveLivePickup(ctx context.Context, userID, liveID string, input models.DeliveryAddress) (*models.DeliveryAddress, error) {
	address, err := normalizePickup(ctx, input)
	if err != nil {
		return nil, err
	}
	err = config.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		live, err := ownedLivestream(tx, userID, liveID)
		if err != nil {
			return err
		}
		if live.Status != "start" {
			return errors.New("livestream_not_active")
		}
		var settings models.ShippingSettings
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&settings, "seller_id = ?", userID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			settings = models.ShippingSettings{SellerID: userID, Pickup: address}
			if err := tx.Create(&settings).Error; err != nil {
				return err
			}
		}
		return tx.Save(&models.LivestreamPickup{LivestreamID: liveID, Address: address}).Error
	})
	if err == nil {
		liveChanged(liveID)
		shippingChanged(liveID)
	}
	return &address, err
}

func PublicLiveOrigin(ctx context.Context, liveID, sellerID string) (any, error) {
	address, err := checkoutPickup(config.DB.WithContext(ctx), OrderInput{LivestreamID: &liveID}, sellerID, false)
	if err != nil {
		if err.Error() == "shipping_pickup_required" {
			return nil, nil
		}
		return nil, err
	}
	// A seller's home coordinates, phone and street are never public room data.
	result := map[string]any{"province_name": address.ProvinceName, "mode": "seller_address"}
	if address.StationID > 0 {
		result["mode"] = "station_dropoff"
		result["station_id"] = address.StationID
		result["station_name"] = address.StationName
		result["station_address"] = address.AddressLine
	}
	return result, nil
}
