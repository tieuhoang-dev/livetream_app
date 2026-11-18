// Package routes registers each domain without opening storage or starting workers.
package routes

import (
	"github.com/gin-gonic/gin"
	"livesteam_app/controller/websocket"
)

func Setup(r *gin.Engine) {
	websocket.Setup(r)
	SetupUserRoutes(r)
	SetupProductRoutes(r)
	SetupOrderRoutes(r)
	SetupSalesRoutes(r)
	SetupLivestreamRoutes(r)
	SetupDeliveryRoutes(r)
}
