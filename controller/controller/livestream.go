package controllers

import (
	"livesteam_app/controller/services"
	"livesteam_app/models"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

var DB *gorm.DB

func SetDatabase(db *gorm.DB) {
	DB = db
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ======================================================
// CLIENT
// ======================================================

type Client struct {
	ID       uuid.UUID
	Username string
	Avatar   string

	Conn *websocket.Conn
	Send chan interface{}

	Done      chan struct{}
	closeOnce sync.Once
}

// ======================================================
// WS MESSAGE
// ======================================================

type WsMessage struct {
	ID         uuid.UUID  `json:"id"`
	SenderID   uuid.UUID  `json:"sender_id"`
	ReceiverID *uuid.UUID `json:"receiver_id"`

	Type      string `json:"type"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	Status    string `json:"status"`

	Page     string `json:"page"`
	Pagesize string `json:"pagesize"`

	TempID string `json:"temp_id"`

	Offer     string `json:"offer"`
	Answer    string `json:"answer"`
	Candidate string `json:"candidate"`
}

// ======================================================
// CLIENT MAP
// ======================================================

var (
	clients      = make(map[uuid.UUID]*Client)
	clientsMutex sync.RWMutex
)

// ======================================================
// STOP CLIENT
// ======================================================

func (c *Client) stop() {
	c.closeOnce.Do(func() {
		close(c.Done)

		_ = c.Conn.Close()
	})
}

// ======================================================
// WRITER
// ======================================================

func (c *Client) writer() {
	pingTicker := time.NewTicker(
		30 * time.Second,
	)

	defer pingTicker.Stop()

	for {
		select {

		case <-c.Done:
			return

		case msg := <-c.Send:
			c.Conn.SetWriteDeadline(
				time.Now().Add(10 * time.Second),
			)

			if err := c.Conn.WriteJSON(msg); err != nil {
				log.Printf(
					"🔴 Write error to %s: %v",
					c.Username,
					err,
				)

				RemoveClient(
					c.ID,
					c,
				)

				return
			}

		case <-pingTicker.C:
			c.Conn.SetWriteDeadline(
				time.Now().Add(10 * time.Second),
			)

			if err := c.Conn.WriteMessage(
				websocket.PingMessage,
				nil,
			); err != nil {

				log.Printf(
					"🔴 Ping error to %s: %v",
					c.Username,
					err,
				)

				RemoveClient(
					c.ID,
					c,
				)

				return
			}
		}
	}
}

// ======================================================
// ADD CLIENT
// ======================================================

func AddClient(
	userID uuid.UUID,
	username string,
	avatar string,
	conn *websocket.Conn,
) *Client {

	c := &Client{
		ID:       userID,
		Username: username,
		Avatar:   avatar,
		Conn:     conn,

		Send: make(
			chan interface{},
			256,
		),

		Done: make(chan struct{}),
	}

	clientsMutex.Lock()

	if old := clients[userID]; old != nil {
		old.stop()
	}

	clients[userID] = c

	clientsMutex.Unlock()

	go c.writer()

	return c
}

// ======================================================
// REMOVE CLIENT
// ======================================================

func RemoveClient(
	userID uuid.UUID,
	expected *Client,
) {

	clientsMutex.Lock()

	current := clients[userID]

	if current == expected {
		delete(
			clients,
			userID,
		)
	}

	clientsMutex.Unlock()

	expected.stop()
}

// ======================================================
// SEND TO USER
// ======================================================

func sendTo(
	userID uuid.UUID,
	payload interface{},
) bool {

	clientsMutex.RLock()

	c := clients[userID]

	clientsMutex.RUnlock()

	if c == nil {
		return false
	}

	select {

	case <-c.Done:
		return false

	default:
	}

	select {

	case <-c.Done:
		return false

	case c.Send <- payload:
		return true

	default:
		log.Printf(
			"🟠 Send buffer full for %s, dropping message",
			userID,
		)

		return false
	}
}

// ======================================================
// HANDLE WEBSOCKET
// ======================================================

func HandleWS(ctx *gin.Context) {

	// ==================================================
	// AUTH
	// ==================================================

	val, exists := ctx.Get("userID")

	if !exists {
		ctx.JSON(
			http.StatusUnauthorized,
			gin.H{
				"error": "Unauthorized",
			},
		)

		return
	}

	uid, ok := val.(uuid.UUID)

	if !ok {
		ctx.JSON(
			http.StatusUnauthorized,
			gin.H{
				"error": "Unauthorized",
			},
		)

		return
	}

	// ==================================================
	// LOAD USER 1 LẦN
	// ==================================================

	var user models.User

	if err := DB.
		Select(
			"id",
			"username",
			"avatar",
		).
		First(
			&user,
			"id = ?",
			uid,
		).Error; err != nil {

		ctx.JSON(
			http.StatusUnauthorized,
			gin.H{
				"error": "User not found",
			},
		)

		return
	}

	// ==================================================
	// UPGRADE WEBSOCKET
	// ==================================================

	ws, err := upgrader.Upgrade(
		ctx.Writer,
		ctx.Request,
		nil,
	)

	if err != nil {
		return
	}

	client := AddClient(
		uid,
		user.Username,
		user.Avatar,
		ws,
	)

	defer RemoveClient(
		uid,
		client,
	)

	log.Printf(
		"🟢 %s connected",
		client.Username,
	)

	// ==================================================
	// WS SETTINGS
	// ==================================================

	ws.SetReadLimit(
		1 << 20,
	)

	ws.SetReadDeadline(
		time.Now().Add(
			70 * time.Second,
		),
	)

	ws.SetPongHandler(
		func(string) error {

			ws.SetReadDeadline(
				time.Now().Add(
					70 * time.Second,
				),
			)

			return nil
		},
	)

	// ==================================================
	// READ LOOP
	// ==================================================

	for {

		var msg WsMessage

		if err := ws.ReadJSON(&msg); err != nil {

			if websocket.IsCloseError(
				err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
			) {

				log.Printf(
					"🔵 %s disconnected",
					client.Username,
				)

			} else {

				log.Printf(
					"🔴 read error %s: %v",
					client.Username,
					err,
				)
			}

			return
		}

		// Không tin sender_id frontend gửi lên.
		// Sender thật lấy từ JWT.
		msg.SenderID = uid

		switch msg.Type {

		// ==================================================
		// CHAT
		// ==================================================

		case "message",
			"text",
			"image",
			"video",
			"voice",
			"file":

			if msg.ReceiverID == nil {

				sendTo(
					uid,
					gin.H{
						"type":   "message_error",
						"tempId": msg.TempID,
						"error":  "receiver_id is required",
					},
				)

				continue
			}

			toID := *msg.ReceiverID

			sendAt := time.Now()

			// ==============================================
			// TEMP ID
			//
			// Phải giữ temp_id client gửi lên để frontend
			// biết message local nào đang được ACK.
			// ==============================================

			tempID := msg.TempID

			if tempID == "" {
				tempID = uuid.New().String()
			}

			// ==============================================
			// MESSAGE ID
			//
			// Tạo ID ngay tại server.
			// Redis và Mongo sau này dùng cùng ID này.
			// ==============================================

			messageID := uuid.New().String()

			// ==============================================
			// PUSH REDIS STREAM
			//
			// Không InsertOne Mongo ở controller nữa.
			// ==============================================

			err := services.PushChatMessage(
				services.QueueChatInput{
					MessageID:  messageID,
					TempID:     tempID,
					SenderID:   uid,
					ReceiverID: toID,
					Type:       msg.Type,
					Content:    msg.Content,
					CreatedAt:  sendAt,
				},
			)

			if err != nil {

				log.Printf(
					"🔴 Redis queue failed [%s -> %s]: %v",
					uid,
					toID,
					err,
				)

				// Redis còn không nhận được thì không broadcast.
				sendTo(
					uid,
					gin.H{
						"type": "message_error",

						"id": messageID,

						"tempId": tempID,

						"error": "Không thể đưa tin nhắn vào hàng đợi",

						"retryable": true,
					},
				)

				continue
			}

			// ==============================================
			// REDIS ACCEPTED
			//
			// Redis đã giữ message → broadcast realtime.
			// ==============================================

			ack := gin.H{
				"type": msg.Type,

				"id": messageID,

				"tempId": tempID,

				"from": uid.String(),

				"to": toID.String(),

				"content": msg.Content,

				"avatar": client.Avatar,

				"username": client.Username,

				"timestamp": sendAt.Format(
					time.RFC3339Nano,
				),

				// queued = Redis đã nhận.
				// Worker chưa chắc đã ghi Mongo xong.
				"status": "queued",
			}

			// Sender nhận ACK ngay
			sendTo(
				uid,
				ack,
			)

			// Receiver nhận realtime
			if toID != uid {
				sendTo(
					toID,
					ack,
				)
			}

		// ==================================================
		// CREATE LIVE
		// ==================================================

		case "create_live":

			var liveStream models.Livestream

			liveStream.ID =
				uuid.New().String()

			liveStream.SellerID =
				uid.String()

			liveStream.Title =
				msg.Content

			liveStream.Status =
				"start"

			liveStream.StartedAt =
				time.Now()

			if err := DB.Create(
				&liveStream,
			).Error; err != nil {

				log.Printf(
					"🔴 Lỗi tạo livestream: %v",
					err,
				)

				sendTo(
					uid,
					gin.H{
						"type": "create_live_error",

						"error": "Không thể tạo livestream",
					},
				)

				continue
			}

			ack := gin.H{
				"type": "create_live_success",

				"id": liveStream.ID,

				"title": liveStream.Title,

				"seller_id": liveStream.SellerID,

				"status": liveStream.Status,

				"message": "Tạo livestream thành công",
			}

			sendTo(
				uid,
				ack,
			)

		// ==================================================
		// END LIVE
		// ==================================================

		case "end_live":

			liveStream, mediaClosed, err := services.EndLivestreamAndMedia(uid.String(), msg.Content)
			if err != nil {
				message := "Không thể kết thúc livestream"
				if err.Error() == "livestream_not_found" || err.Error() == "forbidden" {
					message = "Livestream không tồn tại hoặc không thuộc quyền sở hữu"
				}
				log.Printf("🔴 Lỗi kết thúc livestream: %v", err)
				sendTo(uid, gin.H{"type": "end_live_error", "error": message})
				continue
			}

			ack := gin.H{
				"type": "end_live_success",

				"id": liveStream.ID,

				"title": liveStream.Title,

				"media_closed": mediaClosed,
			}

			sendTo(
				uid,
				ack,
			)

		// ==================================================
		// JOIN LIVE
		// ==================================================

		case "join_live":

			var liveStream models.Livestream

			if err := DB.First(
				&liveStream,
				"id = ? AND status = ?",
				msg.Content,
				"start",
			).Error; err != nil {

				log.Printf(
					"🔴 Lỗi tìm livestream: %v",
					err,
				)

				sendTo(
					uid,
					gin.H{
						"type": "join_live_error",

						"error": "Livestream không tồn tại hoặc đã kết thúc",
					},
				)

				continue
			}

			ack := gin.H{
				"type": "join_live_success",

				"id": liveStream.ID,

				"title": liveStream.Title,
			}

			sendTo(
				uid,
				ack,
			)
		}
	}
}
