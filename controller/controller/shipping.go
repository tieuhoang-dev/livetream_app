package controllers

import (
	"net/http"
	"os"
	"strconv"

	"livesteam_app/config"
	"livesteam_app/controller/services"

	"github.com/gin-gonic/gin"
)

func ShippingStatus(c *gin.Context) {
	provider, err := config.Shipping()
	environment := "staging"
	if provider != nil {
		environment = provider.Environment()
	} else if os.Getenv("GHN_ENV") == "production" {
		environment = "production"
	}
	c.JSON(http.StatusOK, gin.H{"provider": "ghn", "environment": environment, "configured": err == nil, "quote_only": true})
}
func ShippingProvinces(c *gin.Context) {
	rows, err := services.ShippingProvinces(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}
func shippingParent(c *gin.Context, name string) (int, bool) {
	id, err := strconv.Atoi(c.Query(name))
	if err != nil || id <= 0 || id > 10000000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "shipping_address_required"})
		return 0, false
	}
	return id, true
}
func ShippingDistricts(c *gin.Context) {
	id, ok := shippingParent(c, "province_id")
	if !ok {
		return
	}
	provider, err := config.Shipping()
	if err != nil {
		writeServiceError(c, err)
		return
	}
	rows, err := provider.Districts(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	result := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		result = append(result, gin.H{"id": row.ID, "name": row.Name, "support_type": row.SupportType})
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
func ShippingWards(c *gin.Context) {
	id, ok := shippingParent(c, "district_id")
	if !ok {
		return
	}
	provider, err := config.Shipping()
	if err != nil {
		writeServiceError(c, err)
		return
	}
	rows, err := provider.Wards(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	result := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		result = append(result, gin.H{"code": row.Code, "name": row.Name, "support_type": row.SupportType})
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
