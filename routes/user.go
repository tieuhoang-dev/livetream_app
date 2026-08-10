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
	authGroup.GET("/ws", controllers.HandleWS)
}
