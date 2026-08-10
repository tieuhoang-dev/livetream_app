package controllers

import (
	"livesteam_app/controller/services"
	"net/http"

	"github.com/gin-gonic/gin"
)

func OrderProduct(c *gin.Context) {
	var input struct {
		ProductID     string  `json:"product_id" binding:"required"`
		Quantity      int     `json:"quantity" binding:"required"`
		LivestreamID  *string `json:"livestream_id"`
		PaymentMethod string  `json:"payment_method" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := services.OrderProduct(c.GetString("userID"), input.ProductID, input.Quantity, input.LivestreamID, input.PaymentMethod)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Order placed successfully"})
}
