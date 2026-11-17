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
	c.JSON(http.StatusOK, gin.H{"pickup_address": address})
}

func SaveLivePickup(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok || !validID(c, "livestream_id") {
		return
	}
	var input struct {
		Address models.DeliveryAddress `json:"pickup_address"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_delivery_address"})
		return
	}
	address, err := services.SaveLivePickup(c.Request.Context(), uid, c.Param("livestream_id"), input.Address)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pickup_address": address})
}
