package websocket

import (
	"context"

	"github.com/gin-gonic/gin"
	"livesteam_app/config"
	"livesteam_app/controller/services"
	"livesteam_app/internal/realtime"
	"livesteam_app/models"
)

func registerShipping(hub *realtime.Service) {
	register(hub, "shipping.origin.subscribe", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ID string `json:"livestream_id" binding:"required,uuid"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		load := func(context.Context) (realtime.Snapshot, error) {
			return realtime.Snapshot{Data: gin.H{"changed": true}}, nil
		}
		return gin.H{"subscribed": true}, c.Subscribe(req.SubscriptionID, "shipping:"+input.ID, load, true)
	})
	register(hub, "shipping.status", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		provider, err := config.Shipping()
		environment := "staging"
		if provider != nil {
			environment = provider.Environment()
		}
		return gin.H{"provider": "ghn", "environment": environment, "configured": err == nil, "quote_only": true}, nil
	})

	register(hub, "shipping.provinces", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		rows, err := services.ShippingProvinces(ctx)
		return gin.H{"data": rows}, err
	})
	register(hub, "shipping.districts", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ID int `json:"province_id" binding:"required,gt=0,lte=10000000"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		p, err := config.Shipping()
		if err != nil {
			return nil, err
		}
		rows, err := p.Districts(ctx, input.ID)
		data := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			data = append(data, gin.H{"id": row.ID, "name": row.Name, "support_type": row.SupportType})
		}
		return gin.H{"data": data}, err
	})
	register(hub, "shipping.wards", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ID int `json:"district_id" binding:"required,gt=0,lte=10000000"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		p, err := config.Shipping()
		if err != nil {
			return nil, err
		}
		rows, err := p.Wards(ctx, input.ID)
		data := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			data = append(data, gin.H{"code": row.Code, "name": row.Name, "support_type": row.SupportType})
		}
		return gin.H{"data": data}, err
	})
	register(hub, "shipping.stations.nearby", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			Latitude   *float64 `json:"latitude"`
			Longitude  *float64 `json:"longitude"`
			ProvinceID int      `json:"carrier_province_id"`
			DistrictID int      `json:"carrier_district_id"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		if input.Latitude == nil || input.Longitude == nil {
			return nil, realtime.Fail("invalid_coordinates", 400)
		}
		return services.NearbyShippingStations(ctx, *input.Latitude, *input.Longitude, input.ProvinceID, input.DistrictID)
	})
	register(hub, "shipping.pickup.get", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ID string `json:"livestream_id" binding:"required,uuid"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		address, err := services.GetLivePickup(ctx, c.Identity().ID, input.ID)
		return gin.H{"pickup_address": address}, err
	})
	register(hub, "shipping.pickup.save", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input struct {
			ID      string                 `json:"livestream_id" binding:"required,uuid"`
			Address models.DeliveryAddress `json:"pickup_address"`
		}
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		address, err := services.SaveLivePickup(ctx, c.Identity().ID, input.ID, input.Address)
		return gin.H{"pickup_address": address}, err
	})
	register(hub, "shipping.quote", true, func(ctx context.Context, c *realtime.Client, req realtime.Request) (any, error) {
		var input services.OrderInput
		if err := decode(req, &input); err != nil {
			return nil, err
		}
		return services.QuoteOrder(ctx, c.Identity().ID, input)
	})
}
