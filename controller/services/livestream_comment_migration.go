package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"livesteam_app/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gorm.io/gorm"
)

// This shape exists only to read the legacy table during the one-time import.
// It is never AutoMigrated or used to save new comments.
type legacyLivestreamComment struct {
	ID           uint64
	LivestreamID string
	UserID       string
	Username     string
	Content      string
	CreatedAt    time.Time
}

// MigrateLegacyLivestreamComments imports old SQL history before the API starts.
// Mongo stores the resume marker, counters and comments. SQL is read only here.
func MigrateLegacyLivestreamComments(ctx context.Context, db *gorm.DB, collection *mongo.Collection) error {
	table := db.NamingStrategy.TableName("LivestreamComment")
	if !db.Migrator().HasTable(table) {
		return nil
	}
	stateCollection := collection.Database().Collection("chat_migrations")
	key := bson.M{"_id": "mysql-live-comments-v1:" + table}
	var state struct {
		LastID    uint64 `bson:"last_id"`
		Completed bool   `bson:"completed"`
	}
	if err := stateCollection.FindOne(ctx, key).Decode(&state); err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}
	if state.Completed {
		return nil
	}
	var maxID uint64
	if err := db.WithContext(ctx).Table(table).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		return err
	}
	if _, err := collection.Database().Collection("chat_counters").UpdateOne(ctx,
		bson.M{"_id": liveCommentCounterID}, bson.M{"$max": bson.M{"value": maxID}}, options.Update().SetUpsert(true)); err != nil {
		return err
	}
	for {
		var rows []legacyLivestreamComment
		if err := db.WithContext(ctx).Table(table).Where("id > ?", state.LastID).Order("id ASC").Limit(100).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			_, err := stateCollection.UpdateOne(ctx, key, bson.M{"$set": bson.M{"completed": true, "completed_at": time.Now().UTC()}}, options.Update().SetUpsert(true))
			return err
		}
		writes := make([]mongo.WriteModel, 0, len(rows))
		for _, row := range rows {
			comment := models.LivestreamComment{
				DocumentID: liveCommentDocumentID(row.LivestreamID, row.ID), ID: row.ID,
				LivestreamID: row.LivestreamID, UserID: row.UserID, Username: row.Username,
				Content: row.Content, CreatedAt: row.CreatedAt, MessageType: "livestream_comment",
			}
			writes = append(writes, mongo.NewReplaceOneModel().SetFilter(bson.M{"_id": comment.DocumentID}).SetReplacement(comment).SetUpsert(true))
		}
		if _, err := collection.BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(false)); err != nil {
			return fmt.Errorf("import legacy live comments: %w", err)
		}
		state.LastID = rows[len(rows)-1].ID
		if _, err := stateCollection.UpdateOne(ctx, key, bson.M{"$max": bson.M{"last_id": state.LastID}}, options.Update().SetUpsert(true)); err != nil {
			return err
		}
	}
}
