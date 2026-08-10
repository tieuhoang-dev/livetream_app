package config

import (
	"context"
	"log"
	"os"

	"github.com/redis/go-redis/v9"
)

var RedisClient *redis.Client
var Base_Addr = os.Getenv("REDIS_ADDR")
var Password = os.Getenv("REDIS_PASSWORD")

func ConnectRedis() {
	RedisClient = redis.NewClient(&redis.Options{
		Addr:     Base_Addr,
		Password: Password,
		DB:       0,
	})

	if err := RedisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("🔴 Redis connection failed: %v", err)
	}

	log.Println("🟢 Redis connected")
}
