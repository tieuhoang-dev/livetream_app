package services

import (
	"errors"
	"time"

	"livesteam_app/config"
	"livesteam_app/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func lookupError(err error, missing string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(missing)
	}
	return err
}

func OrderProduct(userID, productID string, quantity int, livestreamID *string, paymentMethod string) (*models.Order, error) {
	if quantity <= 0 {
		return nil, errors.New("invalid_quantity")
	}
	var order models.Order
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var price float64
		var stock int
		var updateModel interface{}
		if livestreamID != nil {
			var live models.Livestream
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&live, "id = ?", *livestreamID).Error; err != nil {
				return lookupError(err, "livestream_not_found")
			}
			if live.Status != "start" {
				return errors.New("livestream_not_active")
			}
			var product models.LivestreamProduct
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("livestream_id = ? AND product_id = ? AND status <> ?", *livestreamID, productID, removedLivestreamProduct).
				First(&product).Error; err != nil {
				return lookupError(err, "product_not_found_in_livestream")
			}
			price, stock, updateModel = product.DiscountPrice, product.StockQuantity, &product
		} else {
			var product models.Product
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&product, "id = ?", productID).Error; err != nil {
				return lookupError(err, "product_not_found")
			}
			price, stock, updateModel = product.Price, product.StockQuantity, &product
		}
		if stock < quantity {
			return errors.New("insufficient_stock")
		}
		order = models.Order{ID: uuid.NewString(), UserID: userID, LivestreamID: livestreamID, TotalAmount: price * float64(quantity), Status: "pending", CreatedAt: time.Now()}
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		item := models.OrderItem{ID: uuid.NewString(), OrderID: order.ID, ProductID: productID, Quantity: quantity, PriceAtPurchase: price, CreatedAt: time.Now()}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		payment := models.Payment{ID: uuid.NewString(), OrderID: order.ID, Amount: order.TotalAmount, PaymentMethod: paymentMethod, Status: "pending", CreatedAt: time.Now()}
		if err := tx.Create(&payment).Error; err != nil {
			return err
		}
		return tx.Model(updateModel).Update("stock_quantity", stock-quantity).Error
	})
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func ownedLivestream(tx *gorm.DB, userID, livestreamID string) (*models.Livestream, error) {
	var live models.Livestream
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&live, "id = ?", livestreamID).Error; err != nil {
		return nil, lookupError(err, "livestream_not_found")
	}
	if live.SellerID != userID {
		return nil, errors.New("forbidden")
	}
	return &live, nil
}

// Reserve stock for the live so ordinary and live orders cannot sell the same
// units. Taking a product off sale returns its unsold stock to the catalog.
func AddProductToLivestream(userID, livestreamID, productID string, discountPrice float64, stockQuantity int, isPinned bool) error {
	if stockQuantity <= 0 {
		return errors.New("invalid_quantity")
	}
	if discountPrice < 0 {
		return errors.New("invalid_price")
	}
	return config.DB.Transaction(func(tx *gorm.DB) error {
		live, err := ownedLivestream(tx, userID, livestreamID)
		if err != nil {
			return err
		}
		if live.Status != "start" {
			return errors.New("livestream_not_active")
		}
		var existing int64
		if err := tx.Model(&models.LivestreamProduct{}).
			Where("livestream_id = ? AND product_id = ? AND status <> ?", livestreamID, productID, removedLivestreamProduct).
			Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return errors.New("product_already_in_livestream")
		}
		var product models.Product
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&product, "id = ?", productID).Error; err != nil {
			return lookupError(err, "product_not_found")
		}
		if product.SellerID != userID {
			return errors.New("forbidden")
		}
		if product.StockQuantity < stockQuantity {
			return errors.New("insufficient_stock")
		}
		liveProduct := models.LivestreamProduct{
			ID: uuid.NewString(), LivestreamID: livestreamID, ProductCode: productID,
			DiscountPrice: discountPrice, PriceAtPurchase: product.Price,
			ProductName: product.Name, ProductImage: product.ImageURL, Description: product.Description,
			StockQuantity: stockQuantity, IsPinned: isPinned, Status: "active",
		}
		if err := tx.Create(&liveProduct).Error; err != nil {
			return err
		}
		return tx.Model(&product).Update("stock_quantity", product.StockQuantity-stockQuantity).Error
	})
}

const (
	closedLivestreamProduct  = "closed"
	removedLivestreamProduct = "removed"
)

// EndLivestream returns unreserved stock to the catalog and keeps allocations
// for orders that may still be cancelled or refunded after the live ends.
func EndLivestream(userID, livestreamID string) (*models.Livestream, error) {
	var live *models.Livestream
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		live, err = ownedLivestream(tx, userID, livestreamID)
		if err != nil {
			return err
		}
		var products []models.LivestreamProduct
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("livestream_id = ? AND status NOT IN ?", live.ID, []string{closedLivestreamProduct, removedLivestreamProduct}).
			Order("product_id ASC, id ASC").Find(&products).Error; err != nil {
			return err
		}
		for _, product := range products {
			if err := restoreCatalogStock(tx, product.ProductCode, product.StockQuantity); err != nil {
				return err
			}
			if err := tx.Model(&product).Updates(map[string]interface{}{
				"stock_quantity": 0, "status": closedLivestreamProduct,
			}).Error; err != nil {
				return err
			}
		}
		if live.Status == "end" && live.EndedAt != nil {
			return nil
		}
		endedAt := time.Now()
		live.Status, live.EndedAt = "end", &endedAt
		return tx.Model(live).Updates(map[string]interface{}{"status": live.Status, "ended_at": endedAt}).Error
	})
	if err != nil {
		return nil, err
	}
	return live, nil
}

func restoreCatalogStock(tx *gorm.DB, productID string, quantity int) error {
	if quantity == 0 {
		return nil
	}
	result := tx.Model(&models.Product{}).Where("id = ?", productID).
		UpdateColumn("stock_quantity", gorm.Expr("stock_quantity + ?", quantity))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("product_not_found")
	}
	return nil
}

func RemoveProductFromLivestream(userID, livestreamID, productID string) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		live, err := ownedLivestream(tx, userID, livestreamID)
		if err != nil {
			return err
		}
		if live.Status != "start" {
			return errors.New("livestream_not_active")
		}
		var liveProduct models.LivestreamProduct
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("livestream_id = ? AND product_id = ? AND status <> ?", livestreamID, productID, removedLivestreamProduct).
			First(&liveProduct).Error; err != nil {
			return lookupError(err, "product_not_found_in_livestream")
		}
		var activeOrders int64
		if err := tx.Model(&models.Order{}).Joins("JOIN order_items ON order_items.order_id = orders.id").
			Where("orders.livestream_id = ? AND order_items.product_id = ? AND orders.status IN ?", livestreamID, productID, []string{"pending", "paid"}).Count(&activeOrders).Error; err != nil {
			return err
		}
		if activeOrders > 0 {
			return errors.New("product_has_active_orders")
		}
		if err := restoreCatalogStock(tx, productID, liveProduct.StockQuantity); err != nil {
			return err
		}
		return tx.Model(&liveProduct).Updates(map[string]interface{}{
			"stock_quantity": 0, "status": removedLivestreamProduct,
		}).Error
	})
}

func ownedOrder(tx *gorm.DB, userID, orderID string) (*models.Order, error) {
	var order models.Order
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", orderID, userID).First(&order).Error; err != nil {
		return nil, lookupError(err, "order_not_found")
	}
	return &order, nil
}

func RefundOrder(userID, orderID, action, refundReason string) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		order, err := ownedOrder(tx, userID, orderID)
		if err != nil {
			return err
		}
		switch action {
		case "refund":
			if order.Status != "paid" {
				return errors.New("order_not_paid")
			}
			order.Status, order.RefundReason = "refunded", refundReason
		case "cancel":
			if order.Status != "pending" {
				return errors.New("order_not_pending")
			}
			order.Status = "cancelled"
		default:
			return errors.New("invalid_action")
		}
		if err := tx.Save(order).Error; err != nil {
			return err
		}
		var payment models.Payment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&payment, "order_id = ?", orderID).Error; err != nil {
			return lookupError(err, "payment_not_found")
		}
		if err := tx.Model(&payment).Update("status", order.Status).Error; err != nil {
			return err
		}
		if order.LivestreamID != nil {
			// Use the same lock order as ending the live or changing its allocations.
			var live models.Livestream
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&live, "id = ?", *order.LivestreamID).Error; err != nil {
				return lookupError(err, "livestream_not_found")
			}
		}
		var items []models.OrderItem
		if err := tx.Where("order_id = ?", orderID).Order("product_id ASC, id ASC").Find(&items).Error; err != nil {
			return err
		}
		for _, item := range items {
			if err := restoreOrderItemStock(tx, order.LivestreamID, item); err != nil {
				return err
			}
		}
		return nil
	})
}

func restoreOrderItemStock(tx *gorm.DB, livestreamID *string, item models.OrderItem) error {
	if livestreamID == nil {
		return restoreCatalogStock(tx, item.ProductID, item.Quantity)
	}
	var liveProduct models.LivestreamProduct
	// An allocation can be removed only after all its orders have settled.
	if err := tx.Take(&liveProduct, "livestream_id = ? AND product_id = ? AND status <> ?", *livestreamID, item.ProductID, removedLivestreamProduct).Error; err != nil {
		return lookupError(err, "product_not_found_in_livestream")
	}
	if liveProduct.Status == closedLivestreamProduct {
		return restoreCatalogStock(tx, item.ProductID, item.Quantity)
	}
	return tx.Model(&liveProduct).UpdateColumn("stock_quantity", gorm.Expr("stock_quantity + ?", item.Quantity)).Error
}

// This records the internal payment status; it does not charge a payment gateway.
func PayOrder(userID, orderID, paymentMethod string) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		order, err := ownedOrder(tx, userID, orderID)
		if err != nil {
			return err
		}
		if order.Status != "pending" {
			return errors.New("order_not_pending")
		}
		var payment models.Payment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&payment, "order_id = ?", orderID).Error; err != nil {
			return lookupError(err, "payment_not_found")
		}
		if payment.Status != "pending" {
			return errors.New("order_already_paid")
		}
		if err := tx.Model(&payment).Updates(map[string]interface{}{"payment_method": paymentMethod, "status": "paid"}).Error; err != nil {
			return err
		}
		return tx.Model(order).Update("status", "paid").Error
	})
}
