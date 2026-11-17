package controllers

import (
	"net/http"

	"livesteam_app/controller/services"
	"livesteam_app/models"

	"github.com/gin-gonic/gin"
)

func GetLivePickup(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok || !validID(c, "livestream_id") {
		return
	}
	address, err := services.GetLivePickup(c.Request.Context(), uid, c.Param("livestream_id"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	settings, err := services.GetShippingSettings(c.Request.Context(), uid)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	var rates interface{}
	if settings != nil {
		rates = gin.H{"same_province_fee": settings.SameProvinceFee, "other_province_fee": settings.OtherProvinceFee, "free_shipping_from": settings.FreeShippingFrom}
	}
	c.JSON(http.StatusOK, gin.H{"pickup_address": address, "shipping_configured": settings != nil, "shipping_rates": rates})
}

func SaveLivePickup(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok || !validID(c, "livestream_id") {
		return
	}
	var input struct {
		Address models.DeliveryAddress       `json:"pickup_address"`
		Rates   *services.ShippingRatesInput `json:"shipping_rates"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_delivery_address"})
		return
	}
	address, err := services.SaveLivePickup(c.Request.Context(), uid, c.Param("livestream_id"), input.Address, input.Rates)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pickup_address": address})
}
