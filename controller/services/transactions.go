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
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("livestream_id = ? AND product_id = ?", *livestreamID, productID).First(&product).Error; err != nil {
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
// units. Removing the live product returns its unsold stock to the product.
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
		if err := tx.Model(&models.LivestreamProduct{}).Where("livestream_id = ? AND product_id = ?", livestreamID, productID).Count(&existing).Error; err != nil {
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

func RemoveProductFromLivestream(userID, livestreamID, productID string) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := ownedLivestream(tx, userID, livestreamID); err != nil {
			return err
		}
		var liveProduct models.LivestreamProduct
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("livestream_id = ? AND product_id = ?", livestreamID, productID).First(&liveProduct).Error; err != nil {
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
		result := tx.Model(&models.Product{}).Where("id = ?", productID).UpdateColumn("stock_quantity", gorm.Expr("stock_quantity + ?", liveProduct.StockQuantity))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("product_not_found")
		}
		return tx.Delete(&liveProduct).Error
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
		var items []models.OrderItem
		if err := tx.Where("order_id = ?", orderID).Find(&items).Error; err != nil {
			return err
		}
		for _, item := range items {
			query := tx.Model(&models.Product{}).Where("id = ?", item.ProductID)
			if order.LivestreamID != nil {
				query = tx.Model(&models.LivestreamProduct{}).Where("livestream_id = ? AND product_id = ?", *order.LivestreamID, item.ProductID)
			}
			result := query.UpdateColumn("stock_quantity", gorm.Expr("stock_quantity + ?", item.Quantity))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("product_not_found")
			}
		}
		return nil
	})
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
