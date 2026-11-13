package routes

import (
	controllers "livesteam_app/controller/controller"
	"livesteam_app/middlewares"

	"github.com/gin-gonic/gin"
)

func SetupUserRoutes(r *gin.Engine) {
	userGroup := r.Group("/api/users")
	authGroup := r.Group("/api/auth")
	authGroup.Use(middlewares.AuthMiddleware())
	userGroup.POST("/register", controllers.RegisterUser)
	userGroup.POST("/verify", controllers.VerifyUser)
	userGroup.POST("/login", controllers.Login)
	userGroup.POST("/password/reset-request", controllers.ResetPasswordRequest)
	userGroup.POST("/password/reset", controllers.ResetPassword)

	profileGroup := userGroup.Group("/me", middlewares.AuthMiddleware())
	profileGroup.PUT("", controllers.UpdateProfile)
	profileGroup.POST("/avatar", controllers.UploadAvatar)
	SetupProductRoutes(r)

	orderGroup := r.Group("/api/orders", middlewares.AuthMiddleware())
	orderGroup.GET("", controllers.ListOrders)
	orderGroup.GET("/:order_id", controllers.GetOrder)
	orderGroup.POST("", controllers.OrderProduct)
	orderGroup.POST("/:order_id/pay", controllers.PayOrder)
	orderGroup.POST("/:order_id/cancel", controllers.CancelOrder)
	orderGroup.POST("/:order_id/refund", controllers.RefundOrder)

	salesGroup := r.Group("/api/sales", middlewares.AuthMiddleware())
	salesGroup.GET("/offline", controllers.ListOfflineSales)
	salesGroup.GET("/offline/:order_id", controllers.GetOfflineSale)

	publicLiveGroup := r.Group("/api/livestreams")
	publicLiveGroup.GET("", controllers.ListLivestreams)
	publicLiveGroup.GET("/:livestream_id", controllers.GetLivestream)
	publicLiveGroup.GET("/:livestream_id/products", controllers.ListLivestreamProducts)
	liveGroup := r.Group("/api/livestreams", middlewares.AuthMiddleware())
	liveGroup.POST("/:livestream_id/products", controllers.AddProductToLivestream)
	liveGroup.DELETE("/:livestream_id/products/:product_id", controllers.RemoveProductFromLivestream)
	authGroup.GET("/ws", controllers.HandleWS)
}
