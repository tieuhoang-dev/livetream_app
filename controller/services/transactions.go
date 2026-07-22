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

func OrderProduct(userID string, productID string, quantity int, livestreamID *string, paymentMethod string) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		var price float64
		var stock *int
		var updateModel interface{}

		if livestreamID != nil {
			var liveProd models.LivestreamProduct
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("livestream_id = ? AND product_code = ?", *livestreamID, productID).
				First(&liveProd).Error; err != nil {
				return errors.New("product_not_found_in_livestream")
			}
			price = liveProd.DiscountPrice
			stock = &liveProd.StockQuantity
			updateModel = &liveProd
		} else {
			var prod models.Product
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ?", productID).
				First(&prod).Error; err != nil {
				return errors.New("product_not_found")
			}
			price = prod.Price
			stock = &prod.StockQuantity
			updateModel = &prod
		}

		if *stock < quantity {
			return errors.New("insufficient_stock")
		}

		order := models.Order{
			ID:           uuid.New().String(),
			UserID:       userID,
			LivestreamID: livestreamID,
			TotalAmount:  price * float64(quantity),
			Status:       "pending",
			CreatedAt:    time.Now(),
		}
		if err := tx.Create(&order).Error; err != nil {
			return errors.New("database_error")
		}

		orderItem := models.OrderItem{
			ID:              uuid.New().String(),
			OrderID:         order.ID,
			ProductID:       productID,
			Quantity:        quantity,
			PriceAtPurchase: price,
			CreatedAt:       time.Now(),
		}
		if err := tx.Create(&orderItem).Error; err != nil {
			return errors.New("database_error")
		}
		payment := models.Payment{
			ID:            uuid.New().String(),
			OrderID:       order.ID,
			Amount:        order.TotalAmount,
			PaymentMethod: paymentMethod,
			Status:        "pending",
			CreatedAt:     time.Now(),
		}
		if err := tx.Create(&payment).Error; err != nil {
			return errors.New("database_error")
		}
		return tx.Model(updateModel).Update("stock_quantity", *stock-quantity).Error
	})
}
func AddProductToLivestream(livestreamID, productID string, discountPrice float64, isPinned bool) error {
	var livestream models.Livestream
	if err := config.DB.Where("id =?", livestreamID).First(&livestream).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.New("livestream_not_found")
		}
		return errors.New("database_error")
	}
	live_products := models.LivestreamProduct{
		ID:            uuid.New().String(),
		LivestreamID:  livestreamID,
		ProductCode:   productID,
		DiscountPrice: discountPrice,
		IsPinned:      isPinned,
	}
	if err := config.DB.Create(&live_products).Error; err != nil {
		return errors.New("database_error")
	}
	return nil
}
func RemoveProductFromLivestream(livestreamID, productID string) error {

	return config.DB.Transaction(func(tx *gorm.DB) error {
		var liveProd models.LivestreamProduct

		if err := tx.Where("livestream_id = ? AND product_code = ?", livestreamID, productID).First(&liveProd).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return errors.New("product_not_found_in_livestream")
			}
			return errors.New("database_error")
		}

		if err := tx.Delete(&liveProd).Error; err != nil {
			return errors.New("database_error")
		}

		return nil
	})
}
func PayOrder(orderID string, paymentMethod string) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		var bill models.Payment

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("order_id = ?", orderID).
			First(&bill).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return errors.New("payment_not_found")
			}
			return errors.New("database_error")
		}

		if bill.Status != "pending" {
			return errors.New("order_already_paid")
		}

		bill.PaymentMethod = paymentMethod
		bill.Status = "paid"
		bill.UpdatedAt = time.Now()
		if err := tx.Save(&bill).Error; err != nil {
			return errors.New("database_error")
		}

		if err := tx.Model(&models.Order{}).Where("id = ?", orderID).Update("status", "paid").Error; err != nil {
			return errors.New("database_error")
		}

		return nil
	})
}
