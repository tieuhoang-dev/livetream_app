package services

import (
	"context"
	"errors"

	"livesteam_app/config"
	"livesteam_app/models"

	"gorm.io/gorm"
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
	address, err := normalizeDelivery(input)
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
		return tx.Save(&models.LivestreamPickup{LivestreamID: liveID, Address: address}).Error
	})
	return &address, err
}
