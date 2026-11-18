package routes_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type wsTestClient struct {
	conn  *websocket.Conn
	inbox []map[string]interface{}
}

func dialRealtime(t *testing.T, serverURL, token string) *wsTestClient {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(serverURL, "http")+"/api/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &wsTestClient{conn: conn}
	t.Cleanup(func() { conn.Close() })
	if token != "" {
		client.command(t, "authenticate", map[string]interface{}{"token": token}, 200)
	}
	return client
}
func (c *wsTestClient) read(t *testing.T, accept func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	for i, packet := range c.inbox {
		if accept(packet) {
			c.inbox = append(c.inbox[:i], c.inbox[i+1:]...)
			return packet
		}
	}
	c.conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	for {
		var packet map[string]interface{}
		if err := c.conn.ReadJSON(&packet); err != nil {
			t.Fatal(err)
		}
		if accept(packet) {
			return packet
		}
		c.inbox = append(c.inbox, packet)
	}
}
func (c *wsTestClient) command(t *testing.T, kind string, payload interface{}, want int) map[string]interface{} {
	t.Helper()
	id := uuid.NewString()
	if err := c.conn.WriteJSON(map[string]interface{}{"type": kind, "request_id": id, "payload": payload}); err != nil {
		t.Fatal(err)
	}
	packet := c.read(t, func(p map[string]interface{}) bool { return p["type"] == "result" && p["request_id"] == id })
	status := 200
	if packet["error"] != nil {
		status = int(packet["status"].(float64))
	}
	if status != want {
		t.Fatalf("WS %s: want %d, got %d: %v", kind, want, status, packet)
	}
	if status != 200 {
		return packet
	}
	data, _ := packet["data"].(map[string]interface{})
	return data
}
func (c *wsTestClient) subscribe(t *testing.T, kind string, payload interface{}) string {
	t.Helper()
	id, req := uuid.NewString(), uuid.NewString()
	if err := c.conn.WriteJSON(map[string]interface{}{"type": kind, "request_id": req, "subscription_id": id, "payload": payload}); err != nil {
		t.Fatal(err)
	}
	packet := c.read(t, func(p map[string]interface{}) bool { return p["type"] == "result" && p["request_id"] == req })
	if packet["error"] != nil {
		t.Fatalf("subscribe %s: %v", kind, packet)
	}
	return id
}
func (c *wsTestClient) event(t *testing.T, id, kind string) map[string]interface{} {
	t.Helper()
	return c.read(t, func(p map[string]interface{}) bool {
		return p["type"] == "event" && p["subscription_id"] == id && (kind == "" || p["event"] == kind)
	})
}

func assertLegacySocketRejected(t *testing.T, serverURL, path string) {
	t.Helper()
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(serverURL, "http")+path, nil)
	if err == nil {
		conn.Close()
		t.Fatal("obsolete WebSocket path still upgrades:", path)
	}
	if response == nil {
		t.Fatal("legacy path failed without an HTTP response", err)
	}
	response.Body.Close()
}
