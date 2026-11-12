package main

import (
	"livesteam_app/config"
	"livesteam_app/models"
	"livesteam_app/routes"
	"livesteam_app/teststudio"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	controllers "livesteam_app/controller/controller"

	"livesteam_app/controller/services"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	config.ConnectDatabase()
	models.MigrateAll(config.DB)
	config.ConnectMongoDB()
	config.EnsureMongoIndexes()
	controllers.SetDatabase(config.DB)
	config.ConnectRedis()
	services.StartChatWorkers(50)

	if err != nil {
		log.Fatal(err)
	}

	os.MkdirAll("./uploads", os.ModePerm)
	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		AllowOriginFunc: func(origin string) bool {
			return true
		},
		MaxAge: 12 * time.Hour,
	}))
	r.Static("/uploads", filepath.Join(".", "uploads"))
	r.GET("/teststudio/*path", gin.WrapH(http.StripPrefix("/teststudio", teststudio.Handler())))
	routes.SetupUserRoutes(r)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Fatal(r.Run(":" + port))
}
