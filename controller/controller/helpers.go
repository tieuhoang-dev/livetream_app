package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func currentUserID(c *gin.Context) (string, bool) {
	value, exists := c.Get("userID")
	id, ok := value.(uuid.UUID)
	if !exists || !ok || id == uuid.Nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return "", false
	}
	return id.String(), true
}

func writeServiceError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := err.Error()
	switch message {
	case "user_not_found", "order_not_found", "payment_not_found", "product_not_found", "livestream_not_found", "product_not_found_in_livestream":
		status = http.StatusNotFound
	case "forbidden":
		status = http.StatusForbidden
	case "invalid_quantity", "invalid_price", "invalid_action", "invalid_otp", "otp_expired", "user_not_verified":
		status = http.StatusBadRequest
	case "insufficient_stock", "product_already_in_livestream", "product_has_active_orders", "livestream_not_active", "order_not_paid", "order_not_pending", "order_already_paid", "order_already_processed":
		status = http.StatusConflict
	case "email_delivery_failed":
		status = http.StatusBadGateway
	default:
		message = "Internal server error"
	}
	c.JSON(status, gin.H{"error": message})
}

func validID(c *gin.Context, name string) bool {
	if id, err := uuid.Parse(c.Param(name)); err != nil || id == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid " + name})
		return false
	}
	return true
}
