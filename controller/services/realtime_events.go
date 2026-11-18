package services

import (
	"gorm.io/gorm"
	"livesteam_app/config"
	"livesteam_app/models"
)

func liveChanged(id string) {
	config.Realtime.Publish("lives", "live.changed", nil)
	if id != "" {
		config.Realtime.Publish("live:"+id, "live.changed", nil)
	}
}
func transactLive(id string, fn func(*gorm.DB) error) error {
	err := config.DB.Transaction(fn)
	if err == nil {
		liveChanged(id)
	}
	return err
}
func transactOrder(id string, fn func(*gorm.DB) error) error {
	err := config.DB.Transaction(fn)
	if err == nil {
		orderChanged(id)
	}
	return err
}
func orderChanged(id string) {
	var order models.Order
	if config.DB.Select("id", "user_id", "livestream_id", "status").Take(&order, "id = ?", id).Error != nil {
		return
	}
	payload := map[string]string{"order_id": id, "status": order.Status}
	config.Realtime.Publish("user:"+order.UserID, "order.changed", payload)
	var sellers []string
	config.DB.Model(&models.Product{}).Joins("JOIN order_items ON order_items.product_id = products.id").Where("order_items.order_id = ?", id).Distinct().Pluck("products.seller_id", &sellers)
	for _, seller := range sellers {
		if seller != order.UserID {
			config.Realtime.Publish("user:"+seller, "order.changed", payload)
		}
	}
	if order.LivestreamID != nil {
		liveChanged(*order.LivestreamID)
	}
}

func shippingChanged(liveID string) {
	config.Realtime.Publish("shipping:"+liveID, "shipping.changed", nil)
}
func sellerShippingChanged(sellerID string) {
	var ids []string
	config.DB.Model(&models.Livestream{}).Where("seller_id = ? AND status = ?", sellerID, "start").Pluck("id", &ids)
	for _, id := range ids {
		liveChanged(id)
		shippingChanged(id)
	}
}
func productShippingChanged(productID string) {
	var ids []string
	config.DB.Model(&models.LivestreamProduct{}).Where("product_id = ?", productID).Distinct().Pluck("livestream_id", &ids)
	for _, id := range ids {
		shippingChanged(id)
	}
}
