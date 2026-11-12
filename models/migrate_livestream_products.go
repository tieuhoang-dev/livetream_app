package models

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// Only used to relax a leftover column. It is never included in AutoMigrate,
// so new databases have one reference column: product_id.
type legacyLivestreamProductCode struct {
	ProductCode *string `gorm:"column:product_code;type:char(36)"`
}

// Older schemas use product_id. An intermediate model used product_code;
// AutoMigrate added that column without removing the required product_id,
// causing MySQL 1364 on inserts. Run before AutoMigrate so existing IDs survive.
func migrateLivestreamProductReference(db *gorm.DB) error {
	model := &LivestreamProduct{}
	migrator := db.Migrator()
	if !migrator.HasTable(model) || !migrator.HasColumn(model, "product_code") {
		return nil
	}
	if !migrator.HasColumn(model, "product_id") {
		return migrator.RenameColumn(model, "product_code", "product_id")
	}

	var conflicts int64
	if err := db.Model(model).Where("product_id IS NOT NULL AND product_id <> '' AND product_code IS NOT NULL AND product_code <> '' AND product_id <> product_code").Count(&conflicts).Error; err != nil {
		return err
	}
	if conflicts > 0 {
		return errors.New("livestream_products has conflicting product_id/product_code values; reconcile these rows before restarting")
	}
	if err := db.Model(model).Where("(product_id IS NULL OR product_id = '') AND product_code IS NOT NULL AND product_code <> ''").UpdateColumn("product_id", gorm.Expr("product_code")).Error; err != nil {
		return fmt.Errorf("backfill product_id: %w", err)
	}

	columns, err := migrator.ColumnTypes(model)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name() != "product_code" {
			continue
		}
		if nullable, known := column.Nullable(); known && nullable {
			return nil
		}
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return err
		}
		// Retain legacy values and indexes. Future inserts only supply product_id.
		return db.Table(statement.Table).Migrator().AlterColumn(&legacyLivestreamProductCode{}, "ProductCode")
	}
	return nil
}
