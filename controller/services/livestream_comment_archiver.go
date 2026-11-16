package services

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"livesteam_app/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gorm.io/gorm"
)

const (
	commentArchiveBatchSize = 100 // Maximum per read, never a minimum to flush.
	commentArchiveInterval  = 500 * time.Millisecond
)

// RunLivestreamCommentArchiver copies persisted HTTP live comments to MongoDB.
// The SQL row remains pending until Mongo acknowledges its idempotent upsert.
func RunLivestreamCommentArchiver(ctx context.Context, db *gorm.DB, collection *mongo.Collection) {
	log.Println("🟢 Live comment Mongo archiver started (checks every 500ms)")
	for ctx.Err() == nil {
		count, err := archiveLivestreamComments(ctx, db, collection)
		delay := commentArchiveInterval
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("🔴 Live comment Mongo archive failed; comments remain queued in MySQL: %v", err)
			delay = 2 * time.Second
		} else if count > 0 {
			log.Printf("🟢 Archived %d live comments to MongoDB", count)
			if count == commentArchiveBatchSize {
				continue
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func archiveLivestreamComments(ctx context.Context, db *gorm.DB, collection *mongo.Collection) (int, error) {
	var comments []models.LivestreamComment
	if err := db.WithContext(ctx).Where("mongo_saved_at IS NULL").Order("id ASC").
		Limit(commentArchiveBatchSize).Find(&comments).Error; err != nil {
		return 0, fmt.Errorf("read pending live comments: %w", err)
	}
	if len(comments) == 0 {
		return 0, nil
	}
	writes := make([]mongo.WriteModel, 0, len(comments))
	ids := make([]uint64, 0, len(comments))
	for _, comment := range comments {
		// Stable across retries, process restarts and concurrent API instances.
		id := "live-comment:" + comment.LivestreamID + ":" + strconv.FormatUint(comment.ID, 10)
		doc := bson.M{
			"_id": id, "id": id, "comment_id": strconv.FormatUint(comment.ID, 10),
			"livestream_id": comment.LivestreamID, "sender_id": comment.UserID,
			"username": comment.Username, "message_type": "livestream_comment",
			"content": comment.Content, "created_at": comment.CreatedAt, "status": "sent",
		}
		writes = append(writes, mongo.NewUpdateOneModel().SetFilter(bson.M{"_id": id}).
			SetUpdate(bson.M{"$setOnInsert": doc}).SetUpsert(true))
		ids = append(ids, comment.ID)
	}
	mongoCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := collection.BulkWrite(mongoCtx, writes, options.BulkWrite().SetOrdered(false)); err != nil {
		return 0, fmt.Errorf("save live comments to MongoDB: %w", err)
	}
	if err := db.WithContext(ctx).Model(&models.LivestreamComment{}).
		Where("id IN ? AND mongo_saved_at IS NULL", ids).Update("mongo_saved_at", time.Now().UTC()).Error; err != nil {
		return 0, fmt.Errorf("mark archived live comments: %w", err)
	}
	return len(comments), nil
}
