package routes

import (
	controllers "livesteam_app/controller/http"
	"livesteam_app/middlewares"

	"github.com/gin-gonic/gin"
)

func SetupUserRoutes(r *gin.Engine) {
	userGroup := r.Group("/api/users")
	userGroup.POST("/register", controllers.RegisterUser)
	userGroup.POST("/verify", controllers.VerifyUser)
	userGroup.POST("/login", controllers.Login)
	userGroup.POST("/password/reset-request", controllers.ResetPasswordRequest)
	userGroup.POST("/password/reset", controllers.ResetPassword)

	profileGroup := userGroup.Group("/me", middlewares.AuthMiddleware())
	profileGroup.PUT("", controllers.UpdateProfile)
	profileGroup.POST("/avatar", controllers.UploadAvatar)

}
