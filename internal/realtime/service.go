// Package realtime owns WebSocket connections, requests and subscriptions.
// Domain handlers register here; business services publish here after storage succeeds.
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Identity struct {
	ID, Username, Avatar string
	ExpiresAt            time.Time
}
type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string           { return e.Code }
func Fail(code string, status int) error { return &Error{code, status} }

type Request struct {
	Type           string          `json:"type"`
	RequestID      string          `json:"request_id"`
	SubscriptionID string          `json:"subscription_id,omitempty"`
	Payload        json.RawMessage `json:"payload"`
}
type Packet struct {
	Type           string `json:"type"`
	RequestID      string `json:"request_id,omitempty"`
	SubscriptionID string `json:"subscription_id,omitempty"`
	Event          string `json:"event,omitempty"`
	Data           any    `json:"data,omitempty"`
	Error          string `json:"error,omitempty"`
	Status         int    `json:"status,omitempty"`
}
type Handler func(context.Context, *Client, Request) (any, error)
type Snapshot struct {
	Data any
	More bool
}
type Loader func(context.Context) (Snapshot, error)
type Authenticator func(context.Context, string) (Identity, error)
type Service struct {
	closed       bool
	workers      sync.WaitGroup
	mu           sync.RWMutex
	handlers     map[string]Handler
	topics       map[string]map[*subscription]struct{}
	clients      map[*Client]struct{}
	authenticate Authenticator
}

func New(auth Authenticator) *Service {
	return &Service{handlers: make(map[string]Handler), topics: make(map[string]map[*subscription]struct{}), clients: make(map[*Client]struct{}), authenticate: auth}
}
func (s *Service) Register(name string, handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handlers[name] != nil {
		panic("duplicate realtime handler: " + name)
	}
	s.handlers[name] = handler
}
func (s *Service) Publish(topic, event string, data any) {
	s.mu.RLock()
	subscribers := make([]*subscription, 0, len(s.topics[topic]))
	for sub := range s.topics[topic] {
		subscribers = append(subscribers, sub)
	}
	s.mu.RUnlock()
	for _, sub := range subscribers {
		if sub.refresh {
			sub.trigger()
		} else {
			sub.client.emit(Packet{Type: "event", SubscriptionID: sub.id, Event: event, Data: data})
		}
	}
}
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	clients := make([]*Client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		c.close()
	}
	s.workers.Wait()
}
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Authentication uses a bearer frame, never cookies or a token in the URL.
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	c := &Client{service: s, conn: conn, ctx: ctx, cancel: cancel, send: make(chan Packet, 128), slots: make(chan struct{}, 8), subs: make(map[string]*subscription)}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		c.close()
		return
	}
	s.workers.Add(1)
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	defer s.workers.Done()
	defer func() { c.close(); c.workers.Wait() }()
	conn.SetReadLimit(64 << 10)
	conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(70 * time.Second)) })
	c.workers.Add(1)
	go func() { defer c.workers.Done(); c.writer() }()
	for {
		var req Request
		if err := conn.ReadJSON(&req); err != nil {
			return
		}
		if req.Type == "" || req.RequestID == "" || len(req.RequestID) > 64 || len(req.SubscriptionID) > 64 {
			c.reply(req, nil, Fail("invalid_request", 400))
			continue
		}
		if !c.validSession() {
			return
		}
		if req.Type == "authenticate" {
			c.authenticate(req)
			continue
		}
		if req.Type == "unsubscribe" {
			c.Unsubscribe(req.SubscriptionID)
			c.reply(req, map[string]bool{"unsubscribed": true}, nil)
			continue
		}
		s.mu.RLock()
		handler := s.handlers[req.Type]
		s.mu.RUnlock()
		if handler == nil {
			c.reply(req, nil, Fail("unknown_command", 400))
			continue
		}
		// Registration stays ordered with unsubscribe. Snapshot reads run in the
		// subscription worker, so registering never waits for a database/carrier.
		if strings.HasSuffix(req.Type, ".subscribe") {
			data, err := handler(c.ctx, c, req)
			c.reply(req, data, err)
			continue
		}
		select {
		case c.slots <- struct{}{}:
			c.workers.Add(1)
			go func(req Request) {
				defer c.workers.Done()
				defer func() { <-c.slots }()
				ctx, cancel := context.WithTimeout(c.ctx, 20*time.Second)
				defer cancel()
				data, err := handler(ctx, c, req)
				c.reply(req, data, err)
			}(req)
		default:
			c.reply(req, nil, Fail("too_many_requests", 429))
		}
	}
}

type Client struct {
	workers  sync.WaitGroup
	service  *Service
	conn     *websocket.Conn
	ctx      context.Context
	cancel   context.CancelFunc
	once     sync.Once
	mu       sync.RWMutex
	identity Identity
	subs     map[string]*subscription
	send     chan Packet
	slots    chan struct{}
}

func (c *Client) Identity() Identity { c.mu.RLock(); defer c.mu.RUnlock(); return c.identity }
func (c *Client) validSession() bool {
	id := c.Identity()
	if !id.ExpiresAt.IsZero() && !time.Now().Before(id.ExpiresAt) {
		c.close()
		return false
	}
	return true
}
func (c *Client) authenticate(req Request) {
	var input struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(req.Payload, &input) != nil || input.Token == "" || c.Identity().ID != "" {
		c.reply(req, nil, Fail("unauthorized", 401))
		return
	}
	if c.service.authenticate == nil {
		c.reply(req, nil, Fail("unauthorized", 401))
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	identity, err := c.service.authenticate(ctx, input.Token)
	if err == nil {
		c.mu.Lock()
		c.identity = identity
		c.mu.Unlock()
	}
	c.reply(req, map[string]string{"user_id": identity.ID}, err)
}
func (c *Client) reply(req Request, data any, err error) {
	packet := Packet{Type: "result", RequestID: req.RequestID, Data: data}
	if err != nil {
		packet.Data = nil
		packet.Error, packet.Status = publicError(err)
	}
	c.emit(packet)
}
func publicError(err error) (string, int) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, e.Status
	}
	return "internal_error", 500
}
func (c *Client) emit(packet Packet) {
	if c.ctx.Err() != nil || !c.validSession() {
		return
	}
	select {
	case c.send <- packet:
	default:
		c.close()
	} // Disconnect slow clients; reconnect restores durable snapshots.
}
func (c *Client) writer() {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	defer c.close()
	for {
		select {
		case <-c.ctx.Done():
			return
		case packet := <-c.send:
			if !c.validSession() {
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if c.conn.WriteJSON(packet) != nil {
				return
			}
		case <-ticker.C:
			if !c.validSession() {
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if c.conn.WriteMessage(websocket.PingMessage, nil) != nil {
				return
			}
		}
	}
}
func (c *Client) close() {
	c.once.Do(func() {
		c.cancel()
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.mu.Lock()
		subs := c.subs
		c.subs = make(map[string]*subscription)
		c.mu.Unlock()
		for _, sub := range subs {
			c.service.remove(sub)
		}
		c.service.mu.Lock()
		delete(c.service.clients, c)
		c.service.mu.Unlock()
	})
}
func (c *Client) Subscribe(id, topic string, loader Loader, refresh bool) error {
	if id == "" || len(id) > 64 {
		return Fail("invalid_subscription", 400)
	}
	ctx, cancel := context.WithCancel(c.ctx)
	sub := &subscription{id: id, topic: topic, client: c, loader: loader, refresh: refresh, ctx: ctx, cancel: cancel, changed: make(chan struct{}, 1)}
	c.mu.Lock()
	if c.ctx.Err() != nil {
		c.mu.Unlock()
		cancel()
		return context.Canceled
	}
	old := c.subs[id]
	if old == nil && len(c.subs) >= 16 {
		c.mu.Unlock()
		cancel()
		return Fail("too_many_subscriptions", 429)
	}
	// Hold the client lock through registration, so close/unsubscribe cannot leak a subscription.
	if old != nil {
		c.service.remove(old)
	}
	c.subs[id] = sub
	c.service.mu.Lock()
	if c.service.topics[topic] == nil {
		c.service.topics[topic] = make(map[*subscription]struct{})
	}
	c.service.topics[topic][sub] = struct{}{}
	c.service.mu.Unlock()
	if loader != nil {
		c.workers.Add(1)
	}
	c.mu.Unlock()
	if loader != nil {
		sub.trigger()
		go func() { defer c.workers.Done(); sub.run() }()
	}
	return nil
}
func (c *Client) Unsubscribe(id string) {
	c.mu.Lock()
	sub := c.subs[id]
	delete(c.subs, id)
	c.mu.Unlock()
	if sub != nil {
		c.service.remove(sub)
	}
}
func (s *Service) remove(sub *subscription) {
	sub.cancel()
	s.mu.Lock()
	delete(s.topics[sub.topic], sub)
	if len(s.topics[sub.topic]) == 0 {
		delete(s.topics, sub.topic)
	}
	s.mu.Unlock()
}

type subscription struct {
	id, topic string
	client    *Client
	loader    Loader
	refresh   bool
	ctx       context.Context
	cancel    context.CancelFunc
	changed   chan struct{}
}

func (sub *subscription) trigger() {
	select {
	case sub.changed <- struct{}{}:
	default:
	}
}
func (sub *subscription) run() {
	for {
		select {
		case <-sub.ctx.Done():
			return
		case <-sub.changed:
			for {
				ctx, cancel := context.WithTimeout(sub.ctx, 10*time.Second)
				snapshot, err := sub.loader(ctx)
				cancel()
				if sub.ctx.Err() != nil {
					return
				}
				packet := Packet{Type: "event", SubscriptionID: sub.id, Event: "snapshot", Data: snapshot.Data}
				if err != nil {
					packet.Data = nil
					packet.Error, packet.Status = publicError(err)
				}
				sub.client.emit(packet)
				if err != nil || !snapshot.More {
					break
				}
			}
		}
	}
}
