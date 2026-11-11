package controllers

import (
	"livesteam_app/controller/services"
	"net/http"

	"github.com/gin-gonic/gin"
)

func OrderProduct(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var input struct {
		ProductID     string  `json:"product_id" binding:"required,uuid"`
		Quantity      int     `json:"quantity" binding:"required,gt=0"`
		LivestreamID  *string `json:"livestream_id" binding:"omitempty,uuid"`
		PaymentMethod string  `json:"payment_method" binding:"required,max=50"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	order, err := services.OrderProduct(userID, input.ProductID, input.Quantity, input.LivestreamID, input.PaymentMethod)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Order placed successfully", "order_id": order.ID, "total_amount": order.TotalAmount, "status": order.Status})
}

func PayOrder(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "order_id") {
		return
	}
	var input struct {
		PaymentMethod string `json:"payment_method" binding:"required,max=50"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := services.PayOrder(userID, c.Param("order_id"), input.PaymentMethod); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"order_id": c.Param("order_id"), "status": "paid"})
}

func CancelOrder(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "order_id") {
		return
	}
	if err := services.RefundOrder(userID, c.Param("order_id"), "cancel", ""); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"order_id": c.Param("order_id"), "status": "cancelled"})
}

func RefundOrder(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "order_id") {
		return
	}
	var input struct {
		Reason string `json:"refund_reason" binding:"required,max=255"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := services.RefundOrder(userID, c.Param("order_id"), "refund", input.Reason); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"order_id": c.Param("order_id"), "status": "refunded"})
}

func AddProductToLivestream(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "livestream_id") {
		return
	}
	var input struct {
		ProductID     string   `json:"product_id" binding:"required,uuid"`
		DiscountPrice *float64 `json:"discount_price" binding:"required,gte=0"`
		StockQuantity int      `json:"stock_quantity" binding:"required,gt=0"`
		IsPinned      bool     `json:"is_pinned"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := services.AddProductToLivestream(userID, c.Param("livestream_id"), input.ProductID, *input.DiscountPrice, input.StockQuantity, input.IsPinned); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Product added to livestream"})
}

func RemoveProductFromLivestream(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "livestream_id") || !validID(c, "product_id") {
		return
	}
	if err := services.RemoveProductFromLivestream(userID, c.Param("livestream_id"), c.Param("product_id")); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
