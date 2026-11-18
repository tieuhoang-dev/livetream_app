package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
	"livesteam_app/config"
	"livesteam_app/controller/services"
	"livesteam_app/models"
)

// storage owns only the connections opened by this application instance.
// Business services retain the existing config handles during this refactor.
type storage struct {
	sql   *sql.DB
	mongo *mongo.Client
	redis *redis.Client
}

func openStorage(ctx context.Context) (_ *storage, result error) {
	s := &storage{}
	defer func() {
		if result != nil {
			result = errors.Join(result, s.close())
		}
	}()
	if err := config.ConnectDatabase(ctx); err != nil {
		return nil, fmt.Errorf("connect MySQL: %w", err)
	}
	s.sql, result = config.DB.DB()
	if result != nil {
		return nil, result
	}
	if err := models.MigrateAll(config.DB.WithContext(ctx)); err != nil {
		return nil, fmt.Errorf("migrate MySQL: %w", err)
	}
	if err := config.ConnectMongoDB(ctx); err != nil {
		return nil, fmt.Errorf("connect MongoDB: %w", err)
	}
	s.mongo = config.ChatCollection.Database().Client()
	migrationCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	err := services.MigrateLegacyLivestreamComments(migrationCtx, config.DB, config.ChatCollection)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("migrate legacy comments: %w", err)
	}
	if err := config.EnsureMongoIndexes(ctx); err != nil {
		return nil, fmt.Errorf("ensure MongoDB indexes: %w", err)
	}
	if err := config.ConnectRedis(ctx); err != nil {
		return nil, fmt.Errorf("connect Redis: %w", err)
	}
	s.redis = config.RedisClient
	return s, nil
}

func (s *storage) close() error {
	var result error
	if s.redis != nil {
		result = errors.Join(result, s.redis.Close())
		if config.RedisClient == s.redis {
			config.RedisClient = nil
		}
	}
	if s.mongo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		result = errors.Join(result, s.mongo.Disconnect(ctx))
		cancel()
		if config.ChatCollection != nil && config.ChatCollection.Database().Client() == s.mongo {
			config.ChatCollection = nil
		}
	}
	if s.sql != nil {
		result = errors.Join(result, s.sql.Close())
		if config.DB != nil {
			current, _ := config.DB.DB()
			if current == s.sql {
				config.DB = nil
			}
		}
	}
	return result
}
