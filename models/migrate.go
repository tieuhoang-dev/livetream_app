package models

import (
	"log"

	"gorm.io/gorm"
)
func MigrateAll(db *gorm.DB) {
	err := db.AutoMigrate(
		&User{},
		&Wallet{},
		&Transaction{},
		&Product{},
		&OrderItem{},
		&Order{},
		&LivestreamProduct{},
		&Livestream{},
	)
	
	if err != nil {
		log.Fatalf("MySQL AutoMigrate failed: %v", err)
	}
	
	log.Println("MySQL AutoMigrate completed successfully")
}