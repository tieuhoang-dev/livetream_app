package app

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"livesteam_app/routes"
	"livesteam_app/teststudio"
)

func newRouter() (*gin.Engine, error) {
	if err := os.MkdirAll("uploads", 0755); err != nil {
		return nil, fmt.Errorf("prepare uploads: %w", err)
	}
	router := gin.Default()
	router.Use(cors.New(cors.Config{
		AllowAllOrigins: true,
		AllowMethods:    []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowHeaders:    []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:   []string{"Content-Length"},
		MaxAge:          12 * time.Hour,
	}))
	router.Static("/uploads", filepath.Join(".", "uploads"))
	router.GET("/teststudio/*path", gin.WrapH(http.StripPrefix("/teststudio", teststudio.Handler())))
	routes.Setup(router)
	return router, nil
}
