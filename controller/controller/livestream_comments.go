package controllers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"

	"livesteam_app/controller/services"
	"livesteam_app/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var liveCommentRooms = struct {
	sync.Mutex
	members map[string]map[*Client]struct{}
}{members: make(map[string]map[*Client]struct{})}

// Serialize persistence and delivery in a room so reconnect cursors follow commit order.
var liveCommentWrites [64]sync.Mutex

func removeLiveCommentClient(client *Client) {
	liveCommentRooms.Lock()
	defer liveCommentRooms.Unlock()
	leaveLiveCommentRoom(client)
}

// The caller holds liveCommentRooms.Mutex.
func leaveLiveCommentRoom(client *Client) {
	members := liveCommentRooms.members[client.liveRoom]
	delete(members, client)
	if len(members) == 0 {
		delete(liveCommentRooms.members, client.liveRoom)
	}
	client.liveRoom = ""
}

func joinLiveComments(ctx context.Context, client *Client, liveID string) {
	var live models.Livestream
	if err := DB.WithContext(ctx).Select("id", "title").Take(&live, "id = ? AND status = ?", liveID, "start").Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if !client.enqueue(gin.H{"type": "join_live_error", "error": "Livestream không tồn tại hoặc đã kết thúc"}) {
				client.stop()
			}
		} else {
			log.Printf("WebSocket join live %s: %v", liveID, err)
			client.stop()
		}
		return
	}

	liveCommentRooms.Lock()
	defer liveCommentRooms.Unlock()
	leaveLiveCommentRoom(client)
	members := liveCommentRooms.members[live.ID]
	if members == nil {
		members = make(map[*Client]struct{})
		liveCommentRooms.members[live.ID] = members
	}
	members[client] = struct{}{}
	client.liveRoom = live.ID
	if !client.enqueue(gin.H{"type": "join_live_success", "id": live.ID, "title": live.Title}) {
		client.stop()
	}
}

func sendLiveComment(ctx context.Context, client *Client, msg WsMessage) {
	liveCommentRooms.Lock()
	liveID := client.liveRoom
	liveCommentRooms.Unlock()

	code := "live_comment_not_joined"
	if liveID != "" {
		_, err := saveLiveComment(ctx, liveID, client.ID.String(), client.Username, msg.Content, client, msg.TempID)
		if err == nil {
			return
		}
		switch err.Error() {
		case "invalid_live_comment", "livestream_not_active", "livestream_not_found":
			code = err.Error()
		default:
			code = "live_comment_failed"
			log.Printf("WebSocket save comment in live %s: %v", liveID, err)
		}
	}
	if !client.enqueue(gin.H{"type": "live_comment_error", "error": code, "temp_id": msg.TempID}) {
		client.stop()
	}
}

func saveLiveComment(ctx context.Context, liveID, userID, username, content string, sender *Client, tempID string) (*models.LivestreamComment, error) {
	id, err := uuid.Parse(liveID)
	if err != nil {
		return nil, err
	}
	writes := &liveCommentWrites[int(id[0])%len(liveCommentWrites)]
	writes.Lock()
	defer writes.Unlock()

	comment, err := services.AddLivestreamComment(ctx, id.String(), userID, username, content)
	if err != nil {
		return nil, err
	}

	liveCommentRooms.Lock()
	defer liveCommentRooms.Unlock()
	for client := range liveCommentRooms.members[comment.LivestreamID] {
		message := gin.H{"type": "live_comment", "livestream_id": comment.LivestreamID, "comment": comment}
		if client == sender {
			message["temp_id"] = tempID
		}
		if !client.enqueue(message) {
			client.stop()
		}
	}
	return comment, nil
}

func ListLivestreamComments(c *gin.Context) {
	if !validID(c, "livestream_id") {
		return
	}
	var query struct {
		After uint64 `form:"after"`
	}
	if !bindListQuery(c, &query) {
		return
	}
	comments, hasMore, err := services.ListLivestreamComments(c.Request.Context(), c.Param("livestream_id"), query.After)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	cursor := query.After
	if len(comments) > 0 {
		cursor = comments[len(comments)-1].ID
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": comments, "next_cursor": strconv.FormatUint(cursor, 10), "has_more": hasMore})
}

func AddLivestreamComment(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok || !validID(c, "livestream_id") {
		return
	}
	var input struct {
		Content string `json:"content" binding:"required"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_live_comment"})
		return
	}
	comment, err := saveLiveComment(c.Request.Context(), c.Param("livestream_id"), userID, c.GetString("username"), input.Content, nil, "")
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, comment)
}
