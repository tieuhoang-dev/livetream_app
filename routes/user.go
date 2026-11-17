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
	shippingGroup := r.Group("/api/shipping", middlewares.AuthMiddleware())
	shippingGroup.GET("/status", controllers.ShippingStatus)
	shippingGroup.GET("/provinces", controllers.ShippingProvinces)
	shippingGroup.GET("/districts", controllers.ShippingDistricts)
	shippingGroup.GET("/wards", controllers.ShippingWards)
	shippingGroup.POST("/stations/nearby", controllers.NearbyShippingStations)
	orderGroup.GET("", controllers.ListOrders)
	orderGroup.POST("/quote", controllers.QuoteOrder)
	orderGroup.GET("/:order_id", controllers.GetOrder)
	orderGroup.POST("", controllers.OrderProduct)
	orderGroup.POST("/:order_id/pay", controllers.PayOrder)
	orderGroup.POST("/:order_id/cancel", controllers.CancelOrder)
	orderGroup.POST("/:order_id/refund", controllers.RefundOrder)

	salesGroup := r.Group("/api/sales", middlewares.AuthMiddleware())
	salesGroup.GET("/shipping-settings", controllers.GetShippingSettings)
	salesGroup.PUT("/shipping-settings", controllers.SaveShippingSettings)
	salesGroup.GET("/offline", controllers.ListOfflineSales)
	salesGroup.GET("/offline/:order_id", controllers.GetOfflineSale)
	salesGroup.GET("/orders", controllers.ListSellerOrders)
	salesGroup.GET("/orders/:order_id", controllers.GetSellerOrder)
	salesGroup.POST("/orders/:order_id/pay", controllers.PayOrder)
	salesGroup.POST("/orders/:order_id/refund", controllers.RefundOrder)

	publicLiveGroup := r.Group("/api/livestreams")
	publicLiveGroup.GET("", controllers.ListLivestreams)
	publicLiveGroup.GET("/ws", controllers.LivestreamFeed)
	publicLiveGroup.GET("/:livestream_id", controllers.GetLivestream)
	publicLiveGroup.GET("/:livestream_id/products", controllers.ListLivestreamProducts)
	liveGroup := r.Group("/api/livestreams", middlewares.AuthMiddleware())
	liveGroup.GET("/:livestream_id/comments", controllers.ListLivestreamComments)
	liveGroup.POST("/:livestream_id/comments", controllers.AddLivestreamComment)
	liveGroup.POST("/:livestream_id/media-token", controllers.CreateLiveMediaSession)
	liveGroup.GET("/:livestream_id/pickup", controllers.GetLivePickup)
	liveGroup.PUT("/:livestream_id/pickup", controllers.SaveLivePickup)
	liveGroup.POST("/:livestream_id/end", controllers.EndLive)
	liveGroup.POST("/:livestream_id/products", controllers.AddProductToLivestream)
	liveGroup.DELETE("/:livestream_id/products/:product_id", controllers.RemoveProductFromLivestream)
	authGroup.GET("/ws", controllers.HandleWS)
	r.POST("/api/delivery/reverse-geocode", middlewares.AuthMiddleware(), controllers.ReverseGeocode)
}
