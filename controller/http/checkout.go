package controllers

import (
	"livesteam_app/controller/services"
	"livesteam_app/models"
	"net/http"
)

import "github.com/gin-gonic/gin"

func QuoteOrder(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var input services.OrderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_checkout_request"})
		return
	}
	quote, err := services.QuoteOrder(c.Request.Context(), uid, input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, quote)
}

func GetShippingSettings(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	settings, err := services.GetShippingSettings(c.Request.Context(), uid)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"configured": settings != nil, "settings": settings})
}

func SaveShippingSettings(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		return
	}
	var input struct {
		Pickup models.DeliveryAddress `json:"pickup_address"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_shipping_settings"})
		return
	}
	settings, err := services.SaveShippingSettings(c.Request.Context(), uid, models.ShippingSettings{Pickup: input.Pickup})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"configured": true, "settings": settings})
}
