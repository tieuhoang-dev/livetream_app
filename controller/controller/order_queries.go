package controllers

import (
	"net/http"

	"livesteam_app/controller/services"

	"github.com/gin-gonic/gin"
)

func ListOrders(c *gin.Context) {
	userID, ok := currentUserID(c)
	query := services.OrderQuery{Pagination: defaultPagination()}
	if !ok || !bindListQuery(c, &query) {
		return
	}
	items, total, err := services.ListOrders(userID, query)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writePage(c, query.Pagination, items, total)
}

func GetOrder(c *gin.Context) {
	writeOrderDetail(c, services.GetOrder)
}

func ListSellerOrders(c *gin.Context) {
	userID, ok := currentUserID(c)
	query := services.OrderQuery{Pagination: defaultPagination()}
	if !ok || !bindListQuery(c, &query) {
		return
	}
	items, total, err := services.ListSellerOrders(userID, query)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writePage(c, query.Pagination, items, total)
}

func GetSellerOrder(c *gin.Context) {
	writeOrderDetail(c, services.GetSellerOrder)
}

func ListOfflineSales(c *gin.Context) {
	userID, ok := currentUserID(c)
	query := struct {
		services.Pagination
		Status string `form:"status" binding:"omitempty,oneof=pending paid cancelled refunded"`
	}{Pagination: defaultPagination()}
	if !ok || !bindListQuery(c, &query) {
		return
	}
	items, total, err := services.ListOfflineSales(userID, query.Pagination, query.Status)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	writePage(c, query.Pagination, items, total)
}

func GetOfflineSale(c *gin.Context) {
	writeOrderDetail(c, services.GetOfflineSale)
}

func writeOrderDetail(c *gin.Context, get func(string, string) (*services.OrderDetail, error)) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "order_id") {
		return
	}
	order, err := get(userID, c.Param("order_id"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, order)
}
