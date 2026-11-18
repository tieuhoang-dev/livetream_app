package routes

import (
	"github.com/gin-gonic/gin"
	controllers "livesteam_app/controller/http"
	"livesteam_app/middlewares"
)

func SetupLivestreamRoutes(r *gin.Engine) {
	publicLiveGroup := r.Group("/api/livestreams")
	publicLiveGroup.GET("", controllers.ListLivestreams)
	publicLiveGroup.GET("/:livestream_id", controllers.GetLivestream)
	publicLiveGroup.GET("/:livestream_id/products", controllers.ListLivestreamProducts)
	liveGroup := r.Group("/api/livestreams", middlewares.AuthMiddleware())
	liveGroup.POST("/:livestream_id/media-token", controllers.CreateLiveMediaSession)
	liveGroup.GET("/:livestream_id/pickup", controllers.GetLivePickup)
	liveGroup.PUT("/:livestream_id/pickup", controllers.SaveLivePickup)
	liveGroup.POST("/:livestream_id/end", controllers.EndLive)
	liveGroup.POST("/:livestream_id/products", controllers.AddProductToLivestream)
	liveGroup.DELETE("/:livestream_id/products/:product_id", controllers.RemoveProductFromLivestream)
}
