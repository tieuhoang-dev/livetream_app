package shipping

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func testRequest() QuoteRequest {
	return QuoteRequest{From: Route{DistrictID: 10, WardCode: "001"}, To: Route{DistrictID: 20, WardCode: "002"}, Parcel: Parcel{WeightGrams: 600, LengthCM: 25, WidthCM: 20, HeightCM: 8}, DeclaredValue: 285000}
}
func TestQuoteOnlyClient(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != feePath || r.Method != "POST" || r.Header.Get("Token") != "private-test-token" || r.Header.Get("ShopId") != "123" || r.URL.RawQuery != "" {
			t.Errorf("unexpected carrier request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["from_ward_code"] != "001" || body["to_district_id"] != float64(20) || body["weight"] != float64(600) || body["insurance_value"] != float64(285000) || body["service_type_id"] != float64(2) {
			t.Errorf("wrong quote: %v", body)
		}
		for _, key := range []string{"order_id", "to_name", "to_phone", "to_address", "client_order_code", "cod_amount"} {
			if _, exists := body[key]; exists {
				t.Errorf("quote sent private/order field %s", key)
			}
		}
		w.Write([]byte(`{"code":200,"data":{"total":30900,"service_fee":30000,"insurance_fee":900}}`))
	}))
	defer server.Close()
	client := newClient(Config{Token: "private-test-token", ShopID: 123, Environment: "staging"}, server.URL, server.Client().Transport)
	fee, err := client.Quote(context.Background(), testRequest())
	if err != nil || fee.Total != 30900 || fee.ServiceFee != 30000 || fee.ServiceTypeID != 2 {
		t.Fatalf("wrong fee: %+v %v", fee, err)
	}
	for _, path := range []string{"create", "preview", "cancel", "update", "return", "booking"} {
		req, _ := http.NewRequest("POST", server.URL+"/shiip/public-api/v2/shipping-order/"+path, strings.NewReader(`{}`))
		if _, err := client.http.Do(req); err == nil {
			t.Errorf("forbidden operation %s was sent", path)
		}
	}
	req, _ := http.NewRequest("POST", "https://example.invalid"+feePath, nil)
	if _, err := client.http.Do(req); err == nil {
		t.Fatal("allowed a different host")
	}
	if calls.Load() != 1 {
		t.Fatalf("unexpected network calls: %d", calls.Load())
	}
}

func TestQuoteRejectsRedirects(t *testing.T) {
	var redirected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != feePath {
			redirected.Add(1)
		}
		http.Redirect(w, r, "/shiip/public-api/v2/shipping-order/create", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := newClient(Config{Token: "test", ShopID: 1}, server.URL, server.Client().Transport)
	if _, err := client.Quote(context.Background(), testRequest()); err == nil {
		t.Fatal("accepted redirect")
	}
	if redirected.Load() != 0 {
		t.Fatal("followed redirect to shipment operation")
	}
}

func TestQuoteCODAndHeavyParcelStayOnFeeEndpoint(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != feePath || r.Method != "POST" {
			t.Error("not a quote endpoint")
			w.WriteHeader(500)
			return
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["cod_value"] != float64(285000) || body["service_type_id"] != float64(5) || len(body["items"].([]interface{})) != 1 {
			t.Errorf("incorrect COD/heavy quote: %v", body)
		}
		w.Write([]byte(`{"code":200,"data":{"total":40000,"service_fee":40000}}`))
	}))
	defer server.Close()
	client := newClient(Config{Token: "test", ShopID: 1}, server.URL, server.Client().Transport)
	input := testRequest()
	input.CODValue = 285000
	input.Parcel.WeightGrams = 20000
	fee, err := client.Quote(context.Background(), input)
	if err != nil || fee.ServiceTypeID != 5 || requests != 1 {
		t.Fatal(fee, err, requests)
	}
}

func TestCarrierErrors(t *testing.T) {
	for _, item := range []struct {
		name, body, want string
		status           int
	}{
		{"unavailable", `{"message":"private-test-token"}`, "shipping_unavailable", 503},
		{"rate limited", `{}`, "shipping_unavailable", 429},
		{"authentication", `{}`, "shipping_provider_not_configured", 401},
		{"bad address", `{}`, "shipping_address_unavailable", 400},
		{"missing total", `{"code":200,"data":{"service_fee":100}}`, "shipping_unavailable", 200},
		{"negative total", `{"code":200,"data":{"total":-1}}`, "shipping_unavailable", 200},
		{"bad response", `not-json`, "shipping_unavailable", 200},
		{"provider failure", `{"code":400,"message":"private-test-token","data":{}}`, "shipping_unavailable", 200},
	} {
		t.Run(item.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(item.status); w.Write([]byte(item.body)) }))
			defer server.Close()
			client := newClient(Config{Token: "private-test-token", ShopID: 1}, server.URL, server.Client().Transport)
			_, err := client.Quote(context.Background(), testRequest())
			if err == nil || err.Error() != item.want || strings.Contains(err.Error(), "private-test-token") {
				t.Fatalf("unsafe/wrong error: %v", err)
			}
		})
	}
}

func TestCatalogCacheAndValidation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case provincePath:
			w.Write([]byte(`{"code":200,"data":[{"ProvinceID":1,"ProvinceName":"Hà Nội"}]}`))
		case districtPath:
			if r.URL.Query().Get("province_id") != "1" {
				t.Error("wrong province")
			}
			w.Write([]byte(`{"code":200,"data":[{"DistrictID":10,"DistrictName":"Ba Đình","SupportType":3}]}`))
		case wardPath:
			if r.URL.Query().Get("district_id") != "10" {
				t.Error("wrong district")
			}
			w.Write([]byte(`{"code":200,"data":[{"WardCode":"001","WardName":"Phường thử","SupportType":3}]}`))
		default:
			t.Error("non-catalog call")
		}
	}))
	defer server.Close()
	client := newClient(Config{Token: "test", ShopID: 1}, server.URL, server.Client().Transport)
	for range 2 {
		rows, err := client.Provinces(context.Background())
		if err != nil || len(rows) != 1 {
			t.Fatal(rows, err)
		}
		if _, err := client.Districts(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
		wards, err := client.Wards(context.Background(), 10)
		if err != nil || wards[0].Code != "001" {
			t.Fatal(wards, err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("catalog not cached: %d", calls.Load())
	}
	input := testRequest()
	input.Parcel.WeightGrams = 0
	if _, err := client.Quote(context.Background(), input); err == nil {
		t.Fatal("allowed missing weight")
	}
	input = testRequest()
	input.To.WardCode = ""
	if _, err := client.Quote(context.Background(), input); err == nil {
		t.Fatal("allowed missing area")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Quote(ctx, testRequest()); err == nil {
		t.Fatal("ignored canceled request")
	}
	if calls.Load() != 3 {
		t.Fatal("invalid/canceled request reached provider")
	}
}

func TestConfigDefaultsToStaging(t *testing.T) {
	t.Setenv("GHN_ENV", "")
	t.Setenv("GHN_TOKEN", "test")
	t.Setenv("GHN_SHOP_ID", "123")
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.Environment != "staging" {
		t.Fatal(cfg, err)
	}
	client, err := New(cfg)
	if err != nil || client.base != "https://dev-online-gateway.ghn.vn" {
		t.Fatal("unsafe default")
	}
	t.Setenv("GHN_ENV", "http://localhost/create")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("accepted arbitrary endpoint")
	}
	t.Setenv("GHN_ENV", "staging")
	t.Setenv("GHN_TOKEN", "")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("accepted missing token")
	}
}
