package services

import (
	"context"
	"errors"
	"time"

	"livesteam_app/config"
	"livesteam_app/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ShippingRatesInput struct {
	SameProvinceFee  *float64 `json:"same_province_fee"`
	OtherProvinceFee *float64 `json:"other_province_fee"`
	FreeShippingFrom *float64 `json:"free_shipping_from"`
}

func (rates *ShippingRatesInput) validate() error {
	if rates.SameProvinceFee == nil || rates.OtherProvinceFee == nil {
		return errors.New("invalid_shipping_settings")
	}
	for _, value := range []float64{*rates.SameProvinceFee, *rates.OtherProvinceFee} {
		if _, err := moneyCents(value); err != nil {
			return errors.New("invalid_shipping_settings")
		}
	}
	if rates.FreeShippingFrom != nil {
		if cents, err := moneyCents(*rates.FreeShippingFrom); err != nil || cents <= 0 {
			return errors.New("invalid_shipping_settings")
		}
	}
	return nil
}

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

func SaveLivePickup(ctx context.Context, userID, liveID string, input models.DeliveryAddress, rates *ShippingRatesInput) (*models.DeliveryAddress, error) {
	address, err := normalizeDelivery(input)
	if err != nil {
		return nil, err
	}
	if rates != nil {
		if err := rates.validate(); err != nil {
			return nil, err
		}
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
			if rates == nil {
				return errors.New("shipping_not_configured")
			}
			// A first-time seller configures shipping at broadcast start. Use the
			// confirmed live address as the initial shop origin, without defaults.
			settings = models.ShippingSettings{SellerID: userID, Pickup: address,
				SameProvinceFee: *rates.SameProvinceFee, OtherProvinceFee: *rates.OtherProvinceFee, FreeShippingFrom: rates.FreeShippingFrom}
			if err := tx.Create(&settings).Error; err != nil {
				return err
			}
		} else if rates != nil {
			if err := tx.Model(&settings).Updates(map[string]interface{}{
				"same_province_fee": *rates.SameProvinceFee, "other_province_fee": *rates.OtherProvinceFee,
				"free_shipping_from": rates.FreeShippingFrom, "updated_at": time.Now(),
			}).Error; err != nil {
				return err
			}
		}
		return tx.Save(&models.LivestreamPickup{LivestreamID: liveID, Address: address}).Error
	})
	return &address, err
}
