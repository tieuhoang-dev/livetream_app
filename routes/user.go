package routes

import (
	controllers "livesteam_app/controller/controller"

	"github.com/gin-gonic/gin"
)

func SetupUserRoutes(r *gin.Engine) {
	userGroup := r.Group("/api/users")
	userGroup.POST("/register", controllers.RegisterUser)
	userGroup.POST("/verify", controllers.VerifyUser)
	userGroup.POST("/login", controllers.Login)
}
