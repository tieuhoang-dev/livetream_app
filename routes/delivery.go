package routes

import (
	"github.com/gin-gonic/gin"
	controllers "livesteam_app/controller/http"
	"livesteam_app/middlewares"
)

func SetupDeliveryRoutes(r *gin.Engine) {
	r.POST("/api/delivery/reverse-geocode", middlewares.AuthMiddleware(), controllers.ReverseGeocode)
}
