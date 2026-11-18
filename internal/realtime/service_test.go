package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSlowConsumerClosesAndRemovesSubscriptions(t *testing.T) {
	hub := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{service: hub, ctx: ctx, cancel: cancel, send: make(chan Packet, 1), subs: make(map[string]*subscription)}
	hub.clients[client] = struct{}{}
	if err := client.Subscribe("private", "user:one", nil, false); err != nil {
		t.Fatal(err)
	}
	hub.Publish("user:one", "first", 1)
	hub.Publish("user:one", "second", 2)
	if ctx.Err() == nil {
		t.Fatal("a full send queue silently dropped an event")
	}
	if len(hub.clients) != 0 || len(hub.topics) != 0 {
		t.Fatal("slow connection leaked registrations")
	}
}
func TestSubscriptionLimitAndCloseCancelLoaders(t *testing.T) {
	hub := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{service: hub, ctx: ctx, cancel: cancel, send: make(chan Packet, 128), subs: make(map[string]*subscription)}
	for i := 0; i < 16; i++ {
		if err := client.Subscribe(fmt.Sprint(i), "topic", nil, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Subscribe("overflow", "topic", nil, false); err == nil {
		t.Fatal("unbounded subscriptions")
	}
	client.Unsubscribe("0")
	started, finished := make(chan struct{}), make(chan struct{})
	if err := client.Subscribe("loader", "topic", func(ctx context.Context) (Snapshot, error) {
		close(started)
		<-ctx.Done()
		close(finished)
		return Snapshot{}, ctx.Err()
	}, true); err != nil {
		t.Fatal(err)
	}
	<-started
	client.close()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("loader survived a disconnected client")
	}
	if len(hub.topics) != 0 {
		t.Fatal("topics leaked after close")
	}
}
func TestSharedTransportCorrelatesRequestsAndKeepsUserSessionsIndependent(t *testing.T) {
	hub := New(func(ctx context.Context, token string) (Identity, error) {
		if token != "user-one" {
			return Identity{}, Fail("unauthorized", 401)
		}
		return Identity{ID: token, ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	defer hub.Close()
	hub.Register("echo", func(ctx context.Context, c *Client, r Request) (any, error) {
		var payload any
		json.Unmarshal(r.Payload, &payload)
		return payload, nil
	})
	hub.Register("subscribe", func(ctx context.Context, c *Client, r Request) (any, error) {
		if c.Identity().ID == "" {
			return nil, Fail("unauthorized", 401)
		}
		return nil, c.Subscribe(r.SubscriptionID, "user:"+c.Identity().ID, nil, false)
	})
	server := httptest.NewServer(hub)
	defer server.Close()
	dial := func() *websocket.Conn {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	command := func(c *websocket.Conn, r Request) Packet {
		t.Helper()
		if err := c.WriteJSON(r); err != nil {
			t.Fatal(err)
		}
		var p Packet
		c.SetReadDeadline(time.Now().Add(time.Second))
		if err := c.ReadJSON(&p); err != nil {
			t.Fatal(err)
		}
		if p.RequestID != r.RequestID {
			t.Fatalf("uncorrelated reply: %+v", p)
		}
		return p
	}
	first, second := dial(), dial()
	for _, conn := range []*websocket.Conn{first, second} {
		p := command(conn, Request{Type: "authenticate", RequestID: "auth", Payload: json.RawMessage(`{"token":"user-one"}`)})
		if p.Error != "" {
			t.Fatal(p)
		}
		command(conn, Request{Type: "subscribe", RequestID: "join", SubscriptionID: "messages"})
	}
	hub.Publish("user:user-one", "message", map[string]string{"content": "durable"})
	for _, conn := range []*websocket.Conn{first, second} {
		var packet Packet
		if err := conn.ReadJSON(&packet); err != nil {
			t.Fatal(err)
		}
		if packet.SubscriptionID != "messages" || packet.Event != "message" {
			t.Fatal(packet)
		}
	}
	first.Close()
	p := command(second, Request{Type: "echo", RequestID: "still-alive", Payload: json.RawMessage(`{"ok":true}`)})
	if p.Error != "" {
		t.Fatal(p)
	}
	command(second, Request{Type: "unsubscribe", RequestID: "leave", SubscriptionID: "messages"})
	hub.Publish("user:user-one", "must-not-deliver", nil)
	p = command(second, Request{Type: "echo", RequestID: "barrier", Payload: json.RawMessage(`{}`)})
	if p.Event != "" {
		t.Fatal("unsubscribe did not remove delivery")
	}
}
func TestRequestWorkIsBoundedAndDoesNotBlockReader(t *testing.T) {
	hub := New(nil)
	defer hub.Close()
	release := make(chan struct{})
	defer close(release)
	var running atomic.Int64
	hub.Register("wait", func(ctx context.Context, c *Client, r Request) (any, error) {
		running.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	})
	server := httptest.NewServer(hub)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for i := 0; i < 9; i++ {
		if err := conn.WriteJSON(Request{Type: "wait", RequestID: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	var packet Packet
	if err := conn.ReadJSON(&packet); err != nil {
		t.Fatal(err)
	}
	if packet.Error != "too_many_requests" || packet.Status != 429 || running.Load() > 8 {
		t.Fatalf("unbounded work: %+v, running=%d", packet, running.Load())
	}
}
