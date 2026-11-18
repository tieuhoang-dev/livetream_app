package config

import (
	"livesteam_app/internal/shipping"
	"sync"
)

// ShippingProvider is an injection point for isolated tests. Production uses
// the GHN quote-only client built from backend environment configuration.
var ShippingProvider shipping.Provider
var shippingCache struct {
	sync.Mutex
	cfg    shipping.Config
	client shipping.Provider
}

func Shipping() (shipping.Provider, error) {
	if ShippingProvider != nil {
		return ShippingProvider, nil
	}
	cfg, err := shipping.ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	shippingCache.Lock()
	defer shippingCache.Unlock()
	if shippingCache.client == nil || shippingCache.cfg != cfg {
		client, err := shipping.New(cfg)
		if err != nil {
			return nil, err
		}
		shippingCache.cfg, shippingCache.client = cfg, client
	}
	return shippingCache.client, nil
}
