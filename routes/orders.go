package routes

import (
	"github.com/gin-gonic/gin"
	controllers "livesteam_app/controller/http"
	"livesteam_app/middlewares"
)

func SetupOrderRoutes(r *gin.Engine) {
	orderGroup := r.Group("/api/orders", middlewares.AuthMiddleware())
	orderGroup.GET("", controllers.ListOrders)
	orderGroup.POST("/quote", controllers.QuoteOrder)
	orderGroup.GET("/:order_id", controllers.GetOrder)
	orderGroup.POST("", controllers.OrderProduct)
	orderGroup.POST("/:order_id/pay", controllers.PayOrder)
	orderGroup.POST("/:order_id/cancel", controllers.CancelOrder)
	orderGroup.POST("/:order_id/refund", controllers.RefundOrder)

}
