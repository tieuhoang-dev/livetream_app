package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"livesteam_app/config"
	"livesteam_app/controller/services"
)

// All three addresses must point to disposable test services. No repository
// .env is read, and this test's Mongo database is unique and removed afterward.
func TestApplicationLifecycle(t *testing.T) {
	dsn, mongoURI, redisAddr := os.Getenv("TEST_MYSQL_DSN"), os.Getenv("TEST_MONGO_URI"), os.Getenv("TEST_REDIS_ADDR")
	if dsn == "" || mongoURI == "" || redisAddr == "" {
		t.Skip("set disposable TEST_MYSQL_DSN, TEST_MONGO_URI and TEST_REDIS_ADDR")
	}
	t.Chdir(t.TempDir())
	database := "bootstrap_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	for key, value := range map[string]string{"DATABASE_URL": dsn, "MONGO_URI": mongoURI, "MONGO_DATABASE": database, "REDIS_ADDR": redisAddr, "REDIS_PASSWORD": "", "JWT_SECRET": "bootstrap-test-only", "LIVEKIT_URL": "", "LIVEKIT_API_URL": "", "LIVEKIT_API_KEY": "", "LIVEKIT_API_SECRET": ""} {
		t.Setenv(key, value)
	}
	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	defer gin.SetMode(previousMode)
	oldDB, oldCollection, oldRedis, oldRealtime := config.DB, config.ChatCollection, config.RedisClient, config.Realtime
	defer func() {
		config.DB, config.ChatCollection, config.RedisClient, config.Realtime = oldDB, oldCollection, oldRedis, oldRealtime
	}()
	mongoClient, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mongoClient.Database(database).Drop(ctx)
		mongoClient.Disconnect(ctx)
	})
	queue := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer queue.Close()
	messageID := uuid.NewString()
	if err := queue.XAdd(context.Background(), &redis.XAddArgs{Stream: services.ChatStream, Values: map[string]interface{}{"id": messageID, "sender_id": uuid.NewString(), "receiver_id": uuid.NewString(), "type": "text", "content": "one startup message", "created_at": time.Now().UnixMilli()}}).Err(); err != nil {
		t.Fatal(err)
	}
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reserved.Addr().(*net.TCPAddr).Port
	reserved.Close()
	t.Setenv("PORT", fmt.Sprint(port))
	address := fmt.Sprintf("http://127.0.0.1:%d", port)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx) }()
	stopped := false
	defer func() {
		cancel()
		if !stopped {
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				t.Error("application cleanup hung")
			}
		}
	}()
	client := &http.Client{Timeout: 300 * time.Millisecond}
	ready := false
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		select {
		case err := <-done:
			stopped = true
			t.Fatalf("startup failed: %v", err)
		default:
		}
		response, err := client.Get(address + "/api/products")
		if err == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !ready {
		t.Fatal("API never became ready")
	}
	// HTTP, embedded Studio, CORS and shared WebSocket all use the same router.
	response, err := client.Get(address + "/teststudio/")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("Studio route missing")
	}
	request, _ := http.NewRequest("OPTIONS", address+"/api/products", nil)
	request.Header.Set("Origin", "https://frontend.example.invalid")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 || response.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("CORS preflight failed")
	}
	socket, _, err := websocket.DefaultDialer.Dial(strings.Replace(address, "http://", "ws://", 1)+"/api/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	socket.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := socket.WriteJSON(map[string]interface{}{"type": "lives.list", "request_id": "bootstrap", "payload": map[string]interface{}{}}); err != nil {
		t.Fatal(err)
	}
	var packet map[string]interface{}
	if err := socket.ReadJSON(&packet); err != nil {
		t.Fatal(err)
	}
	if packet["request_id"] != "bootstrap" || packet["error"] != nil {
		body, _ := json.Marshal(packet)
		t.Fatal(string(body))
	}
	collection := mongoClient.Database(database).Collection("chat_logs")
	persisted := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		var document bson.M
		if err := collection.FindOne(context.Background(), bson.M{"_id": messageID}).Decode(&document); err == nil && document["content"] == "one startup message" {
			length, _ := queue.XLen(context.Background(), services.ChatStream).Result()
			if length == 0 {
				persisted = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !persisted {
		t.Fatal("single queued message did not persist to Mongo and leave Redis")
	}
	ownedSQL, _ := config.DB.DB()
	ownedRedis := config.RedisClient
	ownedMongo := config.ChatCollection.Database().Client()
	cancel()
	select {
	case err := <-done:
		stopped = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("shutdown did not join workers")
	}
	if ownedSQL.Ping() == nil || ownedRedis.Ping(context.Background()).Err() == nil || ownedMongo.Ping(context.Background(), nil) == nil {
		t.Fatal("storage connection remained open")
	}
	socket.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := socket.ReadMessage(); err == nil {
		t.Fatal("WebSocket remained open after shutdown")
	}
	if config.DB != nil || config.RedisClient != nil || config.ChatCollection != nil {
		t.Fatal("closed storage handles remain published")
	}
	t.Run("startup failure closes partially opened storage", func(t *testing.T) {
		t.Setenv("MONGO_URI", "://invalid-mongo-test-uri")
		if _, err := openStorage(context.Background()); err == nil {
			t.Fatal("expected Mongo startup failure")
		}
		if config.DB != nil || config.RedisClient != nil || config.ChatCollection != nil {
			t.Fatal("failed startup leaked connections")
		}
	})
}
