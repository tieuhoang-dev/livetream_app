package models

import (
	"fmt"
	"log"

	"gorm.io/gorm"
)

func MigrateAll(db *gorm.DB) error {
	if err := migrateLivestreamProductReference(db); err != nil {
		return fmt.Errorf("livestream product migration: %w", err)
	}
	err := db.AutoMigrate(
		&User{},
		&Wallet{},
		&Transaction{},
		&Product{},
		&OrderItem{},
		&Order{},
		&Payment{},
		&LivestreamProduct{},
		&Livestream{},
		&ShippingSettings{},
		&OrderDelivery{},
		&LivestreamPickup{},
	)

	if err != nil {
		return fmt.Errorf("AutoMigrate: %w", err)
	}

	log.Println("MySQL AutoMigrate completed successfully")
	return nil
}
