package models

import (
	"errors"
	"os"
	"strings"
	"testing"

	sqlmysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func migrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to a disposable MySQL database")
	}
	prefix := "lp_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10] + "_"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy:                           schema.NamingStrategy{TablePrefix: prefix},
		Logger:                                   logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Only remove the tables owned by this test, including on assertion failure.
		db.Migrator().DropTable(&LivestreamProduct{})
		tables, err := db.Migrator().GetTables()
		if err != nil {
			t.Error(err)
		}
		for _, table := range tables {
			if strings.HasPrefix(table, prefix) {
				if err := db.Migrator().DropTable(table); err != nil {
					t.Error(err)
				}
			}
		}
		connection, _ := db.DB()
		connection.Close()
	})
	return db
}

func liveProductTable(t *testing.T, db *gorm.DB) string {
	t.Helper()
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(&LivestreamProduct{}); err != nil {
		t.Fatal(err)
	}
	return statement.Table
}

func legacyLiveProductTable(t *testing.T, db *gorm.DB, referenceColumns string) string {
	t.Helper()
	table := liveProductTable(t, db)
	err := db.Exec(`CREATE TABLE ? (
		id CHAR(36) NOT NULL PRIMARY KEY,
		livestream_id CHAR(36) NOT NULL,
		`+referenceColumns+`,
		discount_price DECIMAL(10,2) NOT NULL,
		price_at_purchase DECIMAL(10,2) NOT NULL,
		product_name VARCHAR(255) NOT NULL,
		product_image VARCHAR(255) NOT NULL,
		description VARCHAR(255) NOT NULL,
		stock_quantity INT NOT NULL,
		is_pinned BOOLEAN NOT NULL,
		status VARCHAR(50) NOT NULL
	)`, clause.Table{Name: table}).Error
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func liveProductFixture() map[string]interface{} {
	return map[string]interface{}{
		"id": uuid.NewString(), "livestream_id": uuid.NewString(),
		"discount_price": 80, "price_at_purchase": 100, "product_name": "Existing snapshot",
		"product_image": "", "description": "Preserve this snapshot", "stock_quantity": 17,
		"is_pinned": true, "status": "active",
	}
}

func TestLivestreamProductReferenceMigration(t *testing.T) {
	for _, shape := range []string{"fresh", "product_id", "product_code", "both", "backfill"} {
		t.Run(shape, func(t *testing.T) {
			db := migrationTestDB(t)
			table := liveProductTable(t, db)
			productID := uuid.NewString()
			row := liveProductFixture()
			var foreignKey string
			if shape != "fresh" {
				columns := "product_id CHAR(36) NOT NULL"
				if shape == "product_code" {
					columns = "product_code CHAR(36) NOT NULL"
				} else if shape == "both" || shape == "backfill" {
					columns += ", product_code CHAR(36) NOT NULL"
				}
				legacyLiveProductTable(t, db, columns)
				if shape == "product_code" {
					row["product_code"] = productID
				} else {
					row["product_id"] = productID
					if shape == "both" {
						row["product_code"] = ""
					} else if shape == "backfill" {
						row["product_id"], row["product_code"] = "", productID
					}
				}
				if err := db.Table(table).Create(row).Error; err != nil {
					t.Fatal(err)
				}
				if shape != "backfill" {
					// Preserve an existing product foreign key through rename/upgrade.
					parent := db.NamingStrategy.TableName("migration_parent")
					if err := db.Exec("CREATE TABLE ? (id CHAR(36) NOT NULL PRIMARY KEY)", clause.Table{Name: parent}).Error; err != nil {
						t.Fatal(err)
					}
					if err := db.Table(parent).Create(map[string]interface{}{"id": productID}).Error; err != nil {
						t.Fatal(err)
					}
					column := "product_id"
					if shape == "product_code" {
						column = "product_code"
					}
					foreignKey = table + "_product_fk"
					if err := db.Exec("ALTER TABLE ? ADD CONSTRAINT ? FOREIGN KEY (?) REFERENCES ? (id)", clause.Table{Name: table}, clause.Column{Name: foreignKey}, clause.Column{Name: column}, clause.Table{Name: parent}).Error; err != nil {
						t.Fatal(err)
					}
				}
				if shape == "both" {
					// Reproduce the reported INSERT before upgrading either column.
					broken := liveProductFixture()
					broken["product_code"] = productID
					err := db.Table(table).Create(broken).Error
					var mysqlError *sqlmysql.MySQLError
					if !errors.As(err, &mysqlError) || mysqlError.Number != 1364 {
						t.Fatalf("expected the reported MySQL 1364 error, got %v", err)
					}
				}
			}

			// This is the production startup path; repeat it to prove idempotence.
			MigrateAll(db)
			MigrateAll(db)
			if !db.Migrator().HasColumn(&LivestreamProduct{}, "product_id") {
				t.Fatal("canonical product_id is missing")
			}
			if shape != "fresh" {
				var saved LivestreamProduct
				if err := db.First(&saved, "id = ?", row["id"]).Error; err != nil {
					t.Fatal(err)
				}
				if saved.ProductCode != productID || saved.StockQuantity != 17 || saved.Description != row["description"] {
					t.Fatalf("existing reference/snapshot changed: %+v", saved)
				}
			}
			if foreignKey != "" && !db.Migrator().HasConstraint(&LivestreamProduct{}, foreignKey) {
				t.Fatal("existing product foreign key was removed")
			}
			newRow := LivestreamProduct{ID: uuid.NewString(), LivestreamID: uuid.NewString(), ProductCode: productID,
				DiscountPrice: 80, PriceAtPurchase: 100, ProductName: "New snapshot", StockQuantity: 10, Status: "active"}
			if err := db.Create(&newRow).Error; err != nil {
				t.Fatalf("insert after migration: %v", err)
			}
			var count int64
			if err := db.Model(&LivestreamProduct{}).Where("id = ? AND product_id = ?", newRow.ID, productID).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("new reference not stored: count=%d error=%v", count, err)
			}
			if shape == "fresh" && db.Migrator().HasColumn(&LivestreamProduct{}, "product_code") {
				t.Fatal("fresh schema should not recreate the legacy column")
			}
		})
	}
}

func TestLivestreamProductReferenceConflict(t *testing.T) {
	db := migrationTestDB(t)
	table := legacyLiveProductTable(t, db, "product_id CHAR(36) NOT NULL, product_code CHAR(36) NOT NULL")
	row := liveProductFixture()
	row["product_id"], row["product_code"] = uuid.NewString(), uuid.NewString()
	if err := db.Table(table).Create(row).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateLivestreamProductReference(db); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("conflicting references must not be overwritten: %v", err)
	}
	var saved struct{ ProductID, ProductCode string }
	if err := db.Table(table).Where("id = ?", row["id"]).Take(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if saved.ProductID != row["product_id"] || saved.ProductCode != row["product_code"] {
		t.Fatal("conflicting data was modified")
	}
}
