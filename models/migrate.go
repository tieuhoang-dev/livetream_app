package models

import (
	"log"

	"gorm.io/gorm"
)

func MigrateAll(db *gorm.DB) {
	if err := migrateLivestreamProductReference(db); err != nil {
		log.Fatalf("MySQL livestream product migration failed: %v", err)
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
		&LivestreamComment{},
	)

	if err != nil {
		log.Fatalf("MySQL AutoMigrate failed: %v", err)
	}

	log.Println("MySQL AutoMigrate completed successfully")
}
