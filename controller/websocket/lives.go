package websocket

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"livesteam_app/controller/services"
	"livesteam_app/internal/realtime"
)

func registerLives(hub *realtime.Service) {
	register(hub, "live.get", false, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ID string `json:"livestream_id" binding:"required,uuid"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		return services.GetLivestream(ctx, input.ID)
	})

	list := func(req realtime.Request) (services.LivestreamQuery, error) {
		q := services.LivestreamQuery{Pagination: services.Pagination{Page: 1, PageSize: 20}, Status: "start", Sort: "newest"}
		err := decode(req, &q)
		return q, err
	}
	page := func(ctx context.Context, q services.LivestreamQuery) (any, error) {
		rows, total, err := services.ListLivestreams(ctx, q)
		return gin.H{"data": rows, "total": total, "page": q.Page, "page_size": q.PageSize}, err
	}
	register(hub, "lives.list", false, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		q, err := list(req)
		if err != nil {
			return nil, err
		}
		return page(ctx, q)
	})
	register(hub, "lives.subscribe", false, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		q, err := list(req)
		if err != nil {
			return nil, err
		}
		err = c.Subscribe(req.SubscriptionID, "lives", snapshot(func(ctx context.Context) (realtime.Snapshot, error) {
			data, err := page(ctx, q)
			return realtime.Snapshot{Data: data}, err
		}), true)
		return gin.H{"subscribed": err == nil}, err
	})
	register(hub, "live.subscribe", false, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		input := struct {
			LivestreamID string `json:"livestream_id" binding:"required,uuid"`
			services.Pagination
		}{Pagination: services.Pagination{Page: 1, PageSize: 12}}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		load := snapshot(func(ctx context.Context) (realtime.Snapshot, error) {
			live, err := services.GetLivestream(ctx, input.LivestreamID)
			if err != nil {
				return realtime.Snapshot{}, err
			}
			rows, total, err := services.ListLivestreamProducts(ctx, input.LivestreamID, input.Pagination)
			if err != nil {
				return realtime.Snapshot{}, err
			}
			origin, err := services.PublicLiveOrigin(ctx, input.LivestreamID, live.SellerID)
			if err != nil {
				return realtime.Snapshot{}, err
			}
			return realtime.Snapshot{Data: gin.H{"live": live, "products": gin.H{"data": rows, "total": total, "page": input.Page, "page_size": input.PageSize}, "shipping_origin": origin}}, nil
		})
		return gin.H{"subscribed": true}, c.Subscribe(req.SubscriptionID, "live:"+input.LivestreamID, load, true)
	})
	register(hub, "live.create", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			Title string `json:"title" binding:"required,max=255"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		input.Title = strings.TrimSpace(input.Title)
		if input.Title == "" {
			return nil, realtime.Fail("invalid_query", 400)
		}
		return services.CreateLivestream(ctx, c.Identity().ID, input.Title)
	})
	register(hub, "live.end", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ID string `json:"livestream_id" binding:"required,uuid"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		live, closed, err := services.EndLivestreamAndMedia(c.Identity().ID, input.ID)
		if err != nil {
			return nil, err
		}
		return gin.H{"id": live.ID, "title": live.Title, "status": live.Status, "media_closed": closed}, nil
	})
	register(hub, "live.media", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ID   string `json:"livestream_id" binding:"required,uuid"`
			Role string `json:"role" binding:"required,oneof=publisher viewer"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		return services.CreateLiveMediaSession(ctx, input.ID, c.Identity().ID, input.Role == "publisher")
	})
	register(hub, "live.products.add", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ID            string   `json:"livestream_id" binding:"required,uuid"`
			ProductID     string   `json:"product_id" binding:"required,uuid"`
			DiscountPrice *float64 `json:"discount_price" binding:"required,gte=0"`
			Stock         int      `json:"stock_quantity" binding:"required,gt=0"`
			Pinned        bool     `json:"is_pinned"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		err := services.AddProductToLivestream(c.Identity().ID, input.ID, input.ProductID, *input.DiscountPrice, input.Stock, input.Pinned)
		return gin.H{"added": err == nil}, err
	})
	register(hub, "live.products.remove", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ID        string `json:"livestream_id" binding:"required,uuid"`
			ProductID string `json:"product_id" binding:"required,uuid"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		err := services.RemoveProductFromLivestream(c.Identity().ID, input.ID, input.ProductID)
		return gin.H{"removed": err == nil}, err
	})
	register(hub, "orders.subscribe", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		return gin.H{"subscribed": true}, c.Subscribe(req.SubscriptionID, "user:"+c.Identity().ID, func(context.Context) (realtime.Snapshot, error) {
			return realtime.Snapshot{Data: gin.H{"resync": true}}, nil
		}, false)
	})
}
