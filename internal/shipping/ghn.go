// Package shipping only reads carrier catalogs and shipping fee quotations.
// It deliberately has no shipment creation, pickup booking or mutation API.
package shipping

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	provincePath = "/shiip/public-api/master-data/province"
	districtPath = "/shiip/public-api/master-data/district"
	wardPath     = "/shiip/public-api/master-data/ward"
	stationPath  = "/shiip/public-api/v2/station/get"
	feePath      = "/shiip/public-api/v2/shipping-order/fee"
)

type Province struct {
	ID   int    `json:"ProvinceID"`
	Name string `json:"ProvinceName"`
}
type District struct {
	ID          int    `json:"DistrictID"`
	Name        string `json:"DistrictName"`
	SupportType int    `json:"SupportType"`
}
type Ward struct {
	Code        string `json:"WardCode"`
	Name        string `json:"WardName"`
	SupportType int    `json:"SupportType"`
}
type Station struct {
	ID           int      `json:"locationId"`
	Name         string   `json:"locationName"`
	Address      string   `json:"address"`
	WardName     string   `json:"wardName"`
	DistrictName string   `json:"districtName"`
	ProvinceName string   `json:"provinceName"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
}
type StationList struct {
	Data     []Station
	Complete bool
}
type Route struct {
	DistrictID int    `json:"district_id"`
	WardCode   string `json:"ward_code"`
	WardName   string `json:"-"`
}
type Parcel struct {
	WeightGrams int `json:"weight_grams"`
	LengthCM    int `json:"length_cm"`
	WidthCM     int `json:"width_cm"`
	HeightCM    int `json:"height_cm"`
}
type QuoteRequest struct {
	From, To      Route
	Parcel        Parcel
	DeclaredValue int64
	CODValue      int64
}
type Fee struct {
	Total         int64 `json:"total"`
	ServiceFee    int64 `json:"service_fee"`
	InsuranceFee  int64 `json:"insurance_fee"`
	ServiceTypeID int   `json:"service_type_id"`
}
type Provider interface {
	Environment() string
	Provinces(context.Context) ([]Province, error)
	Districts(context.Context, int) ([]District, error)
	Wards(context.Context, int) ([]Ward, error)
	Stations(context.Context, int) (StationList, error)
	Quote(context.Context, QuoteRequest) (Fee, error)
}
type Config struct {
	Token       string
	ShopID      int
	Environment string
}

func ConfigFromEnv() (Config, error) {
	shop, _ := strconv.Atoi(os.Getenv("GHN_SHOP_ID"))
	cfg := Config{Token: strings.TrimSpace(os.Getenv("GHN_TOKEN")), ShopID: shop, Environment: strings.TrimSpace(os.Getenv("GHN_ENV"))}
	if cfg.Environment == "" {
		cfg.Environment = "staging"
	}
	if cfg.Token == "" || cfg.ShopID <= 0 || (cfg.Environment != "staging" && cfg.Environment != "production") {
		return cfg, errors.New("shipping_provider_not_configured")
	}
	return cfg, nil
}

type catalogEntry struct {
	body    []byte
	expires time.Time
}
type Client struct {
	cfg   Config
	base  string
	http  *http.Client
	mu    sync.Mutex
	cache map[string]catalogEntry
}

func New(cfg Config) (*Client, error) {
	if cfg.Environment == "" {
		cfg.Environment = "staging"
	}
	if cfg.Token == "" || cfg.ShopID <= 0 {
		return nil, errors.New("shipping_provider_not_configured")
	}
	base := "https://dev-online-gateway.ghn.vn"
	switch cfg.Environment {
	case "staging":
	case "production":
		base = "https://online-gateway.ghn.vn"
	default:
		return nil, errors.New("shipping_provider_not_configured")
	}
	return newClient(cfg, base, http.DefaultTransport), nil
}
func newClient(cfg Config, base string, transport http.RoundTripper) *Client {
	return &Client{cfg: cfg, base: base, http: &http.Client{
		Timeout:       8 * time.Second,
		Transport:     quoteOnlyTransport{base: base, next: transport},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, cache: make(map[string]catalogEntry)}
}
func (c *Client) Environment() string { return c.cfg.Environment }

// This transport is a second boundary: even a future accidental call to an
// unapproved carrier endpoint is rejected before any network request is sent.
type quoteOnlyTransport struct {
	base string
	next http.RoundTripper
}

func (t quoteOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u, _ := url.Parse(t.base)
	allowed := (r.Method == http.MethodGet && (r.URL.Path == provincePath || r.URL.Path == districtPath || r.URL.Path == wardPath || r.URL.Path == stationPath)) || (r.Method == http.MethodPost && r.URL.Path == feePath)
	if !allowed || r.URL.Host != u.Host || r.URL.Scheme != u.Scheme || r.URL.User != nil {
		return nil, errors.New("shipping_operation_not_allowed")
	}
	return t.next.RoundTrip(r)
}
func (c *Client) read(ctx context.Context, method, path string, query url.Values, input interface{}, output interface{}) error {
	u := c.base + path
	if len(query) != 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return errors.New("shipping_invalid_parcel")
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return errors.New("shipping_unavailable")
	}
	req.Header.Set("Token", c.cfg.Token)
	req.Header.Set("ShopId", strconv.Itoa(c.cfg.ShopID))
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return errors.New("shipping_unavailable")
	}
	defer response.Body.Close()
	if path == stationPath && response.StatusCode == 422 {
		return errors.New("shipping_stations_access_required")
	}
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return errors.New("shipping_provider_not_configured")
	}
	if response.StatusCode == 400 || response.StatusCode == 422 {
		return errors.New("shipping_address_unavailable")
	}
	if response.StatusCode != http.StatusOK {
		return errors.New("shipping_unavailable")
	}
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&envelope); err != nil || envelope.Code != 200 || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("shipping_unavailable")
	}
	if err := json.Unmarshal(envelope.Data, output); err != nil {
		return errors.New("shipping_unavailable")
	}
	return nil
}
func (c *Client) catalog(ctx context.Context, path string, query url.Values, output interface{}) error {
	key := path + "?" + query.Encode()
	c.mu.Lock()
	cached, exists := c.cache[key]
	c.mu.Unlock()
	if exists && time.Now().Before(cached.expires) {
		return json.Unmarshal(cached.body, output)
	}
	var data json.RawMessage
	if err := c.read(ctx, http.MethodGet, path, query, nil, &data); err != nil {
		return err
	}
	if err := json.Unmarshal(data, output); err != nil {
		return errors.New("shipping_unavailable")
	}
	c.mu.Lock()
	if len(c.cache) >= 1024 {
		clear(c.cache)
	}
	c.cache[key] = catalogEntry{body: data, expires: time.Now().Add(15 * time.Minute)}
	c.mu.Unlock()
	return nil
}
func (c *Client) Provinces(ctx context.Context) ([]Province, error) {
	var rows []Province
	err := c.catalog(ctx, provincePath, nil, &rows)
	return rows, err
}
func (c *Client) Districts(ctx context.Context, province int) ([]District, error) {
	if province <= 0 {
		return nil, errors.New("shipping_address_required")
	}
	var rows []District
	err := c.catalog(ctx, districtPath, url.Values{"province_id": {strconv.Itoa(province)}}, &rows)
	return rows, err
}
func (c *Client) Wards(ctx context.Context, district int) ([]Ward, error) {
	if district <= 0 {
		return nil, errors.New("shipping_address_required")
	}
	var rows []Ward
	err := c.catalog(ctx, wardPath, url.Values{"district_id": {strconv.Itoa(district)}}, &rows)
	return rows, err
}
func (c *Client) Stations(ctx context.Context, district int) (StationList, error) {
	if district <= 0 {
		return StationList{}, errors.New("shipping_address_required")
	}
	result := StationList{Data: make([]Station, 0)}
	seen := make(map[int]bool)
	// Bound pagination even if the upstream ignores offset. No automatic retry.
	for page := 0; page < 5; page++ {
		var rows []Station
		if err := c.catalog(ctx, stationPath, url.Values{"district_id": {strconv.Itoa(district)}, "offset": {strconv.Itoa(page * 100)}, "limit": {"100"}}, &rows); err != nil {
			return StationList{}, err
		}
		added := 0
		for _, row := range rows {
			if row.ID > 0 && !seen[row.ID] {
				seen[row.ID] = true
				result.Data = append(result.Data, row)
				added++
			}
		}
		if len(rows) < 100 {
			result.Complete = true
			break
		}
		if added == 0 {
			break
		}
	}
	return result, nil
}
func (c *Client) Quote(ctx context.Context, input QuoteRequest) (Fee, error) {
	p := input.Parcel
	if p.WeightGrams <= 0 || p.WeightGrams > 1000000 || p.LengthCM <= 0 || p.WidthCM <= 0 || p.HeightCM <= 0 || p.LengthCM > 300 || p.WidthCM > 300 || p.HeightCM > 300 || input.DeclaredValue < 0 || input.CODValue < 0 {
		return Fee{}, errors.New("shipping_invalid_parcel")
	}
	if input.From.DistrictID <= 0 || input.From.WardCode == "" || input.To.DistrictID <= 0 || input.To.WardCode == "" {
		return Fee{}, errors.New("shipping_address_required")
	}
	service := 2
	if p.WeightGrams >= 20000 {
		service = 5
	}
	// No name, phone, street, or order ID is sent for a quotation.
	body := map[string]interface{}{
		"from_district_id": input.From.DistrictID, "from_ward_code": input.From.WardCode,
		"to_district_id": input.To.DistrictID, "to_ward_code": input.To.WardCode,
		"weight": p.WeightGrams, "length": p.LengthCM, "width": p.WidthCM, "height": p.HeightCM,
		"service_type_id": service, "insurance_value": input.DeclaredValue,
		"cod_value": input.CODValue,
	}
	if service == 5 {
		body["items"] = []map[string]interface{}{{"name": "Kiện báo giá", "quantity": 1, "weight": p.WeightGrams, "length": p.LengthCM, "width": p.WidthCM, "height": p.HeightCM}}
	}
	var result struct {
		Total        *int64 `json:"total"`
		ServiceFee   int64  `json:"service_fee"`
		InsuranceFee int64  `json:"insurance_fee"`
	}
	if err := c.read(ctx, http.MethodPost, feePath, nil, body, &result); err != nil {
		return Fee{}, err
	}
	if result.Total == nil || *result.Total < 0 || *result.Total > 1000000000000 || result.ServiceFee < 0 || result.InsuranceFee < 0 {
		return Fee{}, errors.New("shipping_unavailable")
	}
	return Fee{Total: *result.Total, ServiceFee: result.ServiceFee, InsuranceFee: result.InsuranceFee, ServiceTypeID: service}, nil
}
