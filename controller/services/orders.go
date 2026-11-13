package services

import (
	"database/sql"
	"time"

	"livesteam_app/config"
	"livesteam_app/models"

	"gorm.io/gorm"
)

type OrderQuery struct {
	Pagination
	Channel string `form:"channel" binding:"omitempty,oneof=offline live"`
	Status  string `form:"status" binding:"omitempty,oneof=pending paid cancelled refunded"`
}

type OrderView struct {
	ID           string    `json:"id"`
	LivestreamID *string   `json:"livestream_id"`
	TotalAmount  float64   `json:"total_amount"`
	Status       string    `json:"status"`
	RefundReason string    `json:"refund_reason"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type OrderItemView struct {
	ProductID       string  `json:"product_id"`
	Name            string  `json:"name"`
	ImageURL        string  `json:"image_url"`
	Quantity        int     `json:"quantity"`
	PriceAtPurchase float64 `json:"price_at_purchase"`
}

type OrderPaymentView struct {
	Method string  `json:"payment_method" gorm:"column:payment_method"`
	Status string  `json:"status"`
	Amount float64 `json:"amount"`
}

type OrderDetail struct {
	OrderView
	Items   []OrderItemView   `json:"items"`
	Payment *OrderPaymentView `json:"payment"`
}

func buyerOrders(db *gorm.DB, buyerID string) *gorm.DB {
	return db.Model(&models.Order{}).Where("orders.user_id = ?", buyerID)
}

func sellerOfflineOrders(db *gorm.DB, sellerID string) *gorm.DB {
	items := db.Model(&models.OrderItem{}).Select("1").Where("order_items.order_id = orders.id")
	otherItems := db.Model(&models.OrderItem{}).Select("1").
		Joins("LEFT JOIN products ON products.id = order_items.product_id").
		Where("order_items.order_id = orders.id AND (products.id IS NULL OR products.seller_id <> ?)", sellerID)
	return db.Model(&models.Order{}).Where("orders.livestream_id IS NULL").
		Where("EXISTS (?) AND NOT EXISTS (?)", items, otherItems)
}

func ListOrders(buyerID string, input OrderQuery) ([]OrderView, int64, error) {
	query := buyerOrders(config.DB, buyerID)
	switch input.Channel {
	case "offline":
		query = query.Where("orders.livestream_id IS NULL")
	case "live":
		query = query.Where("orders.livestream_id IS NOT NULL")
	}
	return listOrders(query, input.Pagination, input.Status)
}

func ListOfflineSales(sellerID string, page Pagination, status string) ([]OrderView, int64, error) {
	return listOrders(sellerOfflineOrders(config.DB, sellerID), page, status)
}

func listOrders(query *gorm.DB, page Pagination, status string) ([]OrderView, int64, error) {
	if status != "" {
		query = query.Where("orders.status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]OrderView, 0)
	err := query.Order("orders.created_at DESC, orders.id DESC").Offset(page.Offset()).Limit(page.PageSize).Find(&items).Error
	return items, total, err
}

func GetOrder(buyerID, orderID string) (*OrderDetail, error) {
	return getOrder(orderID, func(tx *gorm.DB) *gorm.DB { return buyerOrders(tx, buyerID) })
}

func GetOfflineSale(sellerID, orderID string) (*OrderDetail, error) {
	return getOrder(orderID, func(tx *gorm.DB) *gorm.DB { return sellerOfflineOrders(tx, sellerID) })
}

func getOrder(orderID string, scope func(*gorm.DB) *gorm.DB) (*OrderDetail, error) {
	detail := OrderDetail{Items: make([]OrderItemView, 0)}
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := scope(tx).Where("orders.id = ?", orderID).Take(&detail.OrderView).Error; err != nil {
			return lookupError(err, "order_not_found")
		}
		if err := tx.Model(&models.OrderItem{}).Where("order_items.order_id = ?", orderID).
			Joins("LEFT JOIN products ON products.id = order_items.product_id").
			Select(`order_items.product_id, order_items.quantity, order_items.price_at_purchase,
				COALESCE(products.name, '') AS name, COALESCE(products.image_url, '') AS image_url`).
			Order("order_items.created_at ASC, order_items.id ASC").Find(&detail.Items).Error; err != nil {
			return err
		}
		var payments []OrderPaymentView
		if err := tx.Model(&models.Payment{}).Where("order_id = ?", orderID).
			Order("created_at ASC, id ASC").Limit(1).Find(&payments).Error; err != nil {
			return err
		}
		if len(payments) > 0 {
			detail.Payment = &payments[0]
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return &detail, nil
}
