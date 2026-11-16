package config

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

var RedisClient *redis.Client

func ConnectRedis() {
	RedisClient = redis.NewClient(&redis.Options{
		// Read after main loads .env, rather than during package initialization.
		Addr:     os.Getenv("REDIS_ADDR"),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       0,
		// 50 chat workers use blocking XREADGROUP connections.
		PoolSize: 100,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := RedisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("🔴 Redis connection failed: %v", err)
	}

	log.Println("🟢 Redis connected")
}
