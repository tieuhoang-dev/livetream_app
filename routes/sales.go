package routes

import (
	"github.com/gin-gonic/gin"
	controllers "livesteam_app/controller/http"
	"livesteam_app/middlewares"
)

func SetupSalesRoutes(r *gin.Engine) {
	salesGroup := r.Group("/api/sales", middlewares.AuthMiddleware())
	salesGroup.GET("/shipping-settings", controllers.GetShippingSettings)
	salesGroup.PUT("/shipping-settings", controllers.SaveShippingSettings)
	salesGroup.GET("/offline", controllers.ListOfflineSales)
	salesGroup.GET("/offline/:order_id", controllers.GetOfflineSale)
	salesGroup.GET("/orders", controllers.ListSellerOrders)
	salesGroup.GET("/orders/:order_id", controllers.GetSellerOrder)
	salesGroup.POST("/orders/:order_id/pay", controllers.PayOrder)
	salesGroup.POST("/orders/:order_id/refund", controllers.RefundOrder)

}
