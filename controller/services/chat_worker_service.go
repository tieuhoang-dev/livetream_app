package services

import (
	"context"
	"fmt"
	"livesteam_app/config"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
)

const (
	ChatStream = "chat_messages"
	ChatGroup  = "chat_workers"

	BatchSize = 100
)

func StartChatWorkers(workerCount int) {
	ctx := context.Background()

	err := config.RedisClient.XGroupCreateMkStream(
		ctx,
		ChatStream,
		ChatGroup,
		"0",
	).Err()

	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		log.Fatalf("🔴 Không tạo được Redis consumer group: %v", err)
	}

	for i := 1; i <= workerCount; i++ {
		go chatWorker(i)
	}

	log.Printf("🟢 Started %d chat workers", workerCount)
}

func chatWorker(workerID int) {
	ctx := context.Background()

	consumerName := fmt.Sprintf(
		"worker-%d",
		workerID,
	)

	for {
		streams, err := config.RedisClient.XReadGroup(
			ctx,
			&redis.XReadGroupArgs{
				Group:    ChatGroup,
				Consumer: consumerName,

				Streams: []string{
					ChatStream,
					">",
				},

				Count: BatchSize,

				Block: 5 * time.Second,
			},
		).Result()

		if err == redis.Nil {
			continue
		}

		if err != nil {
			log.Printf(
				"🔴 Worker %d Redis read error: %v",
				workerID,
				err,
			)

			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			if len(stream.Messages) == 0 {
				continue
			}

			err := saveBatchToMongo(
				stream.Messages,
			)

			if err != nil {
				log.Printf(
					"🔴 Worker %d InsertMany failed batch=%d: %v",
					workerID,
					len(stream.Messages),
					err,
				)

				continue
			}

			redisIDs := make(
				[]string,
				0,
				len(stream.Messages),
			)

			for _, msg := range stream.Messages {
				redisIDs = append(
					redisIDs,
					msg.ID,
				)
			}

			if err := config.RedisClient.XAck(
				ctx,
				ChatStream,
				ChatGroup,
				redisIDs...,
			).Err(); err != nil {
				log.Printf(
					"🔴 Worker %d XACK failed: %v",
					workerID,
					err,
				)
				continue
			}

			if err := config.RedisClient.XDel(
				ctx,
				ChatStream,
				redisIDs...,
			).Err(); err != nil {
				log.Printf(
					"🔴 Worker %d XDEL failed: %v",
					workerID,
					err,
				)
				continue
			}

			log.Printf(
				"🟢 Worker %d saved + removed %d messages",
				workerID,
				len(redisIDs),
			)
			log.Printf(
				"🟢 Worker %d saved %d messages",
				workerID,
				len(stream.Messages),
			)
		}
	}
}

func saveBatchToMongo(
	messages []redis.XMessage,
) error {

	if len(messages) == 0 {
		return nil
	}

	docs := make(
		[]interface{},
		0,
		len(messages),
	)

	for _, message := range messages {
		doc, err := redisMessageToMongoDocument(
			message,
		)

		if err != nil {
			return err
		}

		docs = append(
			docs,
			doc,
		)
	}

	mongoCtx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)

	defer cancel()

	_, err := config.ChatCollection.InsertMany(
		mongoCtx,
		docs,
	)

	return err
}

func redisMessageToMongoDocument(
	message redis.XMessage,
) (bson.M, error) {

	getString := func(key string) string {
		value, exists := message.Values[key]

		if !exists {
			return ""
		}

		return fmt.Sprint(value)
	}

	messageID := getString("id")

	if messageID == "" {
		return nil, fmt.Errorf(
			"missing message id redisID=%s",
			message.ID,
		)
	}

	createdAtMS, err := strconv.ParseInt(
		getString("created_at"),
		10,
		64,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"invalid created_at redisID=%s: %w",
			message.ID,
			err,
		)
	}

	return bson.M{
		"_id": messageID,

		"id": messageID,

		"sender_id": getString("sender_id"),

		"receiver_id": getString("receiver_id"),

		"message_type": getString("type"),

		"content": getString("content"),

		"temp_id": getString("temp_id"),

		"status": "sent",

		"created_at": time.UnixMilli(createdAtMS),
	}, nil
}
