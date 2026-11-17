package controllers

import (
	"net/http"

	"livesteam_app/controller/services"

	"github.com/gin-gonic/gin"
)

func ReverseGeocode(c *gin.Context) {
	if _, ok := currentUserID(c); !ok {
		return
	}
	var input struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Latitude == nil || input.Longitude == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_coordinates"})
		return
	}
	address, err := services.ReverseGeocode(c.Request.Context(), *input.Latitude, *input.Longitude)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"address": address, "attribution": "© OpenStreetMap contributors"})
}
