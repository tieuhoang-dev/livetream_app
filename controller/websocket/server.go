// Package websocket adapts domain services to the shared realtime service.
// It does not own connections, write pumps or global client maps.
package websocket

import (
	"context"
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"livesteam_app/config"
	"livesteam_app/internal/apperrors"
	"livesteam_app/internal/realtime"
	"livesteam_app/middlewares"
)

func Setup(r *gin.Engine) {
	hub := realtime.New(func(ctx context.Context, token string) (realtime.Identity, error) {
		user, expiry, err := middlewares.Authenticate(ctx, token)
		if err != nil {
			return realtime.Identity{}, realtime.Fail("unauthorized", 401)
		}
		return realtime.Identity{ID: user.ID, Username: user.Username, Avatar: user.Avatar, ExpiresAt: expiry}, nil
	})
	config.Realtime = hub
	registerLives(hub)
	registerComments(hub)
	registerShipping(hub)
	registerChat(hub)
	r.GET("/api/ws", gin.WrapH(hub))
}
func register(hub *realtime.Service, name string, private bool, fn realtime.Handler) {
	hub.Register(name, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		if private && c.Identity().ID == "" {
			return nil, realtime.Fail("unauthorized", 401)
		}
		data, err := fn(ctx, c, req)
		return data, publicError(err)
	})
}
func publicError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*realtime.Error); ok {
		return err
	}
	status, message := apperrors.Public(err)
	return realtime.Fail(message, status)
}
func decode(req realtime.Request, out any) error {
	if len(req.Payload) == 0 {
		req.Payload = json.RawMessage(`{}`)
	}
	if json.Unmarshal(req.Payload, out) != nil || binding.Validator.ValidateStruct(out) != nil {
		return realtime.Fail("invalid_query", 400)
	}
	return nil
}
func snapshot(fn func(context.Context) (realtime.Snapshot, error)) realtime.Loader {
	return func(ctx context.Context) (realtime.Snapshot, error) {
		data, err := fn(ctx)
		return data, publicError(err)
	}
}
