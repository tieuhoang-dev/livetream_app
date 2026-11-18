package routes

import (
	controllers "livesteam_app/controller/http"
	"livesteam_app/middlewares"

	"github.com/gin-gonic/gin"
)

func SetupProductRoutes(r *gin.Engine) {
	public := r.Group("/api/products")
	public.GET("", controllers.ListProducts)
	public.GET("/:product_id", controllers.GetProduct)
	owner := r.Group("/api/products", middlewares.AuthMiddleware())
	owner.POST("", controllers.CreateProduct)
	owner.POST("/images", controllers.UploadProductImage)
	owner.PATCH("/:product_id", controllers.UpdateProduct)
	owner.DELETE("/:product_id", controllers.DeleteProduct)
	owner.POST("/:product_id/stock", controllers.AdjustProductStock)
}
