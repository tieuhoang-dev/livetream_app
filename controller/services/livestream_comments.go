package services

import (
	"context"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"livesteam_app/config"
	"livesteam_app/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const commentPageSize = 50
const liveCommentCounterID = "livestream_comments"

func ListLivestreamComments(ctx context.Context, liveID string, after uint64) ([]models.LivestreamComment, bool, error) {
	if after > math.MaxInt64 {
		return nil, false, errors.New("invalid_pagination")
	}
	// SQL holds the live relationship; comment content is read only from Mongo.
	if err := config.DB.WithContext(ctx).Select("id").Take(&models.Livestream{}, "id = ?", liveID).Error; err != nil {
		return nil, false, lookupError(err, "livestream_not_found")
	}
	if config.ChatCollection == nil {
		return nil, false, errors.New("chat_storage_unavailable")
	}
	filter := bson.M{"livestream_id": liveID, "message_type": "livestream_comment", "sequence": bson.M{"$gt": int64(after)}}
	direction, limit := 1, int64(commentPageSize+1)
	if after == 0 {
		direction, limit = -1, commentPageSize
	}
	mongoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cursor, err := config.ChatCollection.Find(mongoCtx, filter, options.Find().SetSort(bson.D{{Key: "sequence", Value: direction}}).SetLimit(limit))
	if err != nil {
		return nil, false, err
	}
	defer cursor.Close(mongoCtx)
	comments := make([]models.LivestreamComment, 0, limit)
	if err := cursor.All(mongoCtx, &comments); err != nil {
		return nil, false, err
	}
	if after == 0 {
		slices.Reverse(comments)
	}
	hasMore := len(comments) > commentPageSize
	if hasMore {
		comments = comments[:commentPageSize]
	}
	return comments, hasMore, nil
}

func AddLivestreamComment(ctx context.Context, liveID, userID, username, content string) (*models.LivestreamComment, error) {
	content = strings.TrimSpace(content)
	if content == "" || utf8.RuneCountInString(content) > 300 {
		return nil, errors.New("invalid_live_comment")
	}
	if config.ChatCollection == nil {
		return nil, errors.New("chat_storage_unavailable")
	}
	comment := &models.LivestreamComment{
		LivestreamID: liveID, UserID: userID, Username: username, Content: content, MessageType: "livestream_comment",
	}
	err := config.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var live models.Livestream
		// Hold the relational live lock against EndLivestream while Mongo saves.
		// No comment record or content is written to SQL.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").Take(&live, "id = ?", liveID).Error; err != nil {
			return lookupError(err, "livestream_not_found")
		}
		if live.Status != "start" {
			return errors.New("livestream_not_active")
		}
		mongoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var counter struct{ Value uint64 }
		err := config.ChatCollection.Database().Collection("chat_counters").FindOneAndUpdate(
			mongoCtx, bson.M{"_id": liveCommentCounterID}, bson.M{"$inc": bson.M{"value": int64(1)}},
			options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
		).Decode(&counter)
		if err != nil {
			return err
		}
		comment.ID = counter.Value
		comment.DocumentID = liveCommentDocumentID(liveID, comment.ID)
		comment.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)
		_, err = config.ChatCollection.InsertOne(mongoCtx, comment)
		return err
	})
	return comment, err
}

func liveCommentDocumentID(liveID string, id uint64) string {
	return "live-comment:" + liveID + ":" + strconv.FormatUint(id, 10)
}
