package services

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"livesteam_app/config"
	"livesteam_app/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const commentPageSize = 50

func ListLivestreamComments(ctx context.Context, liveID string, after uint64) ([]models.LivestreamComment, bool, error) {
	db := config.DB.WithContext(ctx)
	if err := db.Select("id").Take(&models.Livestream{}, "id = ?", liveID).Error; err != nil {
		return nil, false, lookupError(err, "livestream_not_found")
	}
	query := db.Where("livestream_id = ?", liveID)
	limit := commentPageSize
	if after == 0 {
		query = query.Order("id DESC")
	} else {
		query = query.Where("id > ?", after).Order("id ASC")
		limit++
	}
	comments := make([]models.LivestreamComment, 0, limit)
	if err := query.Limit(limit).Find(&comments).Error; err != nil {
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
	comment := &models.LivestreamComment{UserID: userID, Username: username, Content: content}
	err := config.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var live models.Livestream
		// Serialize writes in a room with each other and with EndLivestream.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").Take(&live, "id = ?", liveID).Error; err != nil {
			return lookupError(err, "livestream_not_found")
		}
		if live.Status != "start" {
			return errors.New("livestream_not_active")
		}
		comment.LivestreamID = live.ID
		return tx.Create(comment).Error
	})
	return comment, err
}
