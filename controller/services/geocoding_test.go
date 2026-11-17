package services

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReverseGeocoder(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.Contains(r.UserAgent(), "LiveMart") || r.URL.Query().Get("accept-language") != "vi" || r.URL.Query().Get("lat") != "21.028511" || r.URL.Query().Get("lon") != "105.804817" {
			t.Errorf("wrong provider request: %s %s", r.UserAgent(), r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"address": map[string]string{"country_code": "vn", "city": "Hà Nội", "suburb": "Phường Hoàn Kiếm", "road": "Đinh Tiên Hoàng", "house_number": "12"}})
	}))
	defer server.Close()
	g := &reverseGeocoder{client: server.Client(), endpoint: server.URL, gate: make(chan struct{}, 1), cache: make(map[string]cachedLocation)}
	for range 2 {
		address, err := g.lookup(context.Background(), 21.028511, 105.804817)
		if err != nil || address.ProvinceCode != "1" || address.Ward != "Phường Hoàn Kiếm" || address.AddressLine != "12 Đinh Tiên Hoàng" || address.LocationMode != "current" || *address.Latitude != 21.028511 {
			t.Fatalf("wrong resolved address: %+v %v", address, err)
		}
	}
	if calls != 1 {
		t.Fatalf("cache missed: %d requests", calls)
	}
	for _, point := range [][2]float64{{91, 105}, {21, -181}, {math.NaN(), 0}, {0, math.Inf(1)}} {
		if _, err := g.lookup(context.Background(), point[0], point[1]); err == nil || err.Error() != "invalid_coordinates" {
			t.Fatalf("accepted bad coordinates: %v %v", point, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g.gate <- struct{}{}
	if _, err := g.lookup(ctx, 21, 105); err != context.Canceled {
		t.Fatalf("canceled lookup waited in queue: %v", err)
	}
	<-g.gate
}

func TestReverseGeocoderFailures(t *testing.T) {
	for _, item := range []struct {
		name, body, want string
		status           int
	}{
		{"provider unavailable", `{}`, "geocoding_unavailable", 503},
		{"bad json", `not json`, "geocoding_unavailable", 200},
		{"outside Vietnam", `{"address":{"country_code":"us","state":"California"}}`, "location_not_supported", 200},
		{"unknown province", `{"address":{"country_code":"vn","state":"Unknown"}}`, "location_not_supported", 200},
	} {
		t.Run(item.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(item.status)
				w.Write([]byte(item.body))
			}))
			defer server.Close()
			g := &reverseGeocoder{client: server.Client(), endpoint: server.URL, gate: make(chan struct{}, 1), cache: make(map[string]cachedLocation)}
			if _, err := g.lookup(context.Background(), 21, 105); err == nil || err.Error() != item.want {
				t.Fatalf("want %s, got %v", item.want, err)
			}
		})
	}
}

func TestReverseProvince(t *testing.T) {
	for name, code := range map[string]string{"Hanoi": "1", "Ho Chi Minh City": "79", "Tỉnh Bình Dương": "79", "Thừa Thiên Huế": "46", "Ba Ria - Vung Tau Province": "79", "Tỉnh Hà Giang": "8", "Đà Nẵng": "48", "Đắk Lắk": "66"} {
		if actual := reverseProvince(name); actual != code {
			t.Errorf("%s: want %s got %s", name, code, actual)
		}
	}
}
