package config

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

var RedisClient *redis.Client

func ConnectRedis(ctx context.Context) error {
	client := redis.NewClient(&redis.Options{
		// Read after main loads .env, rather than during package initialization.
		Addr:     os.Getenv("REDIS_ADDR"),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       0,
		// 50 chat workers use blocking XREADGROUP connections.
		PoolSize: 100,
	})

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return err
	}

	RedisClient = client
	log.Println("Redis connected")
	return nil
}
