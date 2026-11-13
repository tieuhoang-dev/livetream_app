package controllers

import (
	"net/http"

	"livesteam_app/controller/services"

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
	case "forbidden", "cannot_buy_own_product":
		status = http.StatusForbidden
	case "invalid_quantity", "invalid_price", "invalid_action", "invalid_otp", "otp_expired", "user_not_verified", "invalid_product", "empty_update", "invalid_pagination", "invalid_live_comment", "invalid_refund_reason":
		status = http.StatusBadRequest
	case "insufficient_stock", "product_already_in_livestream", "product_has_active_orders", "livestream_not_active", "order_not_paid", "order_not_pending", "order_already_paid", "order_already_processed", "product_in_use":
		status = http.StatusConflict
	case "live_media_unavailable":
		status = http.StatusServiceUnavailable
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

func defaultPagination() services.Pagination {
	return services.Pagination{Page: 1, PageSize: 20}
}

func bindListQuery(c *gin.Context, query interface{}) bool {
	if err := c.ShouldBindQuery(query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_query"})
		return false
	}
	return true
}

func writePage[T any](c *gin.Context, page services.Pagination, items []T, total int64) {
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page.Page, "page_size": page.PageSize})
}
