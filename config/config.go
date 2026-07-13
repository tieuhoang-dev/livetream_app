package config

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB
var ChatCollection *mongo.Collection

func ConnectDatabase() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	dsn := strings.TrimPrefix(databaseURL, "mysql://")
	dsn = strings.Replace(dsn, "@", "@tcp(", 1)
	dsn = strings.Replace(dsn, "/", ")/", 1)
	dsn += "?charset=utf8mb4&parseTime=True&loc=Local"

	// Logger để debug query
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Info,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)

	database, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   newLogger,
	})

	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		log.Fatal("Failed to get generic database object:", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := sqlDB.Ping(); err != nil {
		log.Fatal("Database unreachable:", err)
	}

	DB = database
	log.Println("Database connection established")
}
func ConnectMongoDB() {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		log.Fatal("MONGO_URI is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().ApplyURI(mongoURI)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatal("Failed to connect to MongoDB:", err)
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal("MongoDB unreachable:", err)
	}

	ChatCollection = client.Database("livestream_app").Collection("chat_logs")
	log.Println("MongoDB connection established")
}

func EnsureMongoIndexes() {
	if ChatCollection == nil {
		log.Println("MongoDB ChatCollection is not initialized")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	indexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "livestream_id", Value: 1},
			{Key: "created_at", Value: 1},
		},
	}

	_, err := ChatCollection.Indexes().CreateOne(ctx, indexModel)
	if err != nil {
		log.Printf("Warning: Failed to create index for ChatCollection: %v", err)
	} else {
		log.Println("MongoDB Indexes ensured successfully")
	}
}
