package shipping

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestStationCatalogIsReadOnlyAndPaginated(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != stationPath || r.URL.Query().Get("district_id") != "1444" || r.URL.Query().Get("limit") != "100" || r.Header.Get("Token") != "fixture-token" {
			t.Errorf("wrong station request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		rows := make([]Station, 0)
		for i := offset; i < offset+100 && i < 101; i++ {
			rows = append(rows, Station{ID: i + 1, Name: "Fixture"})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "data": rows})
	}))
	defer srv.Close()
	c := newClient(Config{Token: "fixture-token", ShopID: 1, Environment: "staging"}, srv.URL, srv.Client().Transport)
	for range 2 {
		rows, err := c.Stations(context.Background(), 1444)
		if err != nil || len(rows.Data) != 101 || !rows.Complete {
			t.Fatalf("station pages: %+v %v", rows, err)
		}
	}
	if calls != 2 {
		t.Fatalf("catalog cache missed: %d", calls)
	}
	for _, path := range []string{stationPath, "/shiip/public-api/v2/shipping-order/create", "/shiip/public-api/v2/shipping-order/preview"} {
		req, _ := http.NewRequest("POST", srv.URL+path, nil)
		if _, err := c.http.Do(req); err == nil {
			t.Fatalf("allowed mutation %s", path)
		}
	}
	if calls != 2 {
		t.Fatal("blocked mutation reached network")
	}
}

func TestStationAccessErrorAndPaginationBound(t *testing.T) {
	for _, status := range []int{422, 200} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				if status == 422 {
					w.Write([]byte(`{"code_message":"SECURITY_CHECK_FAILED","message":"secret-token"}`))
					return
				}
				rows := make([]Station, 100)
				for i := range rows {
					rows[i] = Station{ID: i + 1, Name: "Repeated page"}
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "data": rows})
			}))
			defer srv.Close()
			c := newClient(Config{Token: "fixture-token", ShopID: 1}, srv.URL, srv.Client().Transport)
			if _, err := c.Stations(context.Background(), 0); err == nil || calls != 0 {
				t.Fatal("invalid district reached provider")
			}
			rows, err := c.Stations(context.Background(), 1)
			if status == 422 {
				if err == nil || err.Error() != "shipping_stations_access_required" {
					t.Fatalf("wrong access error: %v", err)
				}
				return
			}
			if err != nil || rows.Complete || calls != 2 || len(rows.Data) != 100 {
				t.Fatalf("unbounded/repeated pagination: %+v %v calls=%d", rows, err, calls)
			}
		})
	}
}
