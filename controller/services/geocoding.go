package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"livesteam_app/models"

	"golang.org/x/text/unicode/norm"
)

type cachedLocation struct {
	address models.DeliveryAddress
	expires time.Time
}
type reverseGeocoder struct {
	client   *http.Client
	endpoint string
	gate     chan struct{}
	next     time.Time
	cache    map[string]cachedLocation
}

var locationGeocoder = &reverseGeocoder{
	client: &http.Client{Timeout: 8 * time.Second}, gate: make(chan struct{}, 1),
	cache: make(map[string]cachedLocation),
}

func ReverseGeocode(ctx context.Context, latitude, longitude float64) (models.DeliveryAddress, error) {
	return locationGeocoder.lookup(ctx, latitude, longitude)
}

func (g *reverseGeocoder) lookup(ctx context.Context, latitude, longitude float64) (models.DeliveryAddress, error) {
	if math.IsNaN(latitude) || math.IsInf(latitude, 0) || latitude < -90 || latitude > 90 || math.IsNaN(longitude) || math.IsInf(longitude, 0) || longitude < -180 || longitude > 180 {
		return models.DeliveryAddress{}, errors.New("invalid_coordinates")
	}
	// One request per second to the public provider, with bounded in-memory
	// caching. A canceled browser request must not wait behind the queue.
	select {
	case g.gate <- struct{}{}:
		defer func() { <-g.gate }()
	case <-ctx.Done():
		return models.DeliveryAddress{}, ctx.Err()
	}
	key := fmt.Sprintf("%.6f,%.6f", latitude, longitude)
	if item, ok := g.cache[key]; ok && time.Now().Before(item.expires) {
		result := item.address
		result.Latitude, result.Longitude = &latitude, &longitude
		return result, nil
	}
	if delay := time.Until(g.next); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return models.DeliveryAddress{}, ctx.Err()
		}
	}
	endpoint := g.endpoint
	if endpoint == "" {
		endpoint = os.Getenv("GEOCODING_REVERSE_URL")
	}
	if endpoint == "" {
		endpoint = "https://nominatim.openstreetmap.org/reverse"
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return models.DeliveryAddress{}, errors.New("geocoding_unavailable")
	}
	query := u.Query()
	query.Set("format", "jsonv2")
	query.Set("lat", strconv.FormatFloat(latitude, 'f', 6, 64))
	query.Set("lon", strconv.FormatFloat(longitude, 'f', 6, 64))
	query.Set("addressdetails", "1")
	query.Set("accept-language", "vi")
	query.Set("zoom", "18")
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return models.DeliveryAddress{}, errors.New("geocoding_unavailable")
	}
	req.Header.Set("User-Agent", "LiveMart/1.0 (+https://github.com/tieuhoang-dev/livetream_app)")
	req.Header.Set("Accept", "application/json")
	g.next = time.Now().Add(time.Second)
	response, err := g.client.Do(req)
	if err != nil {
		return models.DeliveryAddress{}, errors.New("geocoding_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return models.DeliveryAddress{}, errors.New("geocoding_unavailable")
	}
	var result struct {
		DisplayName string            `json:"display_name"`
		Address     map[string]string `json:"address"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(&result); err != nil {
		return models.DeliveryAddress{}, errors.New("geocoding_unavailable")
	}
	a := result.Address
	code := reverseProvince(a["state"], a["province"], a["city"], a["region"])
	if strings.ToLower(a["country_code"]) != "vn" || code == "" {
		return models.DeliveryAddress{}, errors.New("location_not_supported")
	}
	street := strings.TrimSpace(a["house_number"] + " " + firstAddressPart(a["road"], a["pedestrian"], a["residential"]))
	if street == "" {
		street = result.DisplayName
	}
	address := models.DeliveryAddress{
		LocationMode: "current", ProvinceCode: code, ProvinceName: provinceNames[code],
		Ward:        limitedAddress(firstAddressPart(a["quarter"], a["suburb"], a["village"], a["town"]), 100),
		AddressLine: limitedAddress(street, 255), Latitude: &latitude, Longitude: &longitude,
	}
	if len(g.cache) >= 256 {
		clear(g.cache)
	}
	g.cache[key] = cachedLocation{address: address, expires: time.Now().Add(10 * time.Minute)}
	return address, nil
}

func firstAddressPart(parts ...string) string {
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			return value
		}
	}
	return ""
}
func limitedAddress(value string, limit int) string {
	letters := []rune(strings.TrimSpace(value))
	if len(letters) > limit {
		letters = letters[:limit]
	}
	return string(letters)
}
func provinceKey(value string) string {
	var key strings.Builder
	for _, letter := range norm.NFD.String(strings.ToLower(value)) {
		if letter == 'đ' {
			letter = 'd'
		}
		if unicode.IsLetter(letter) && !unicode.Is(unicode.Mn, letter) {
			key.WriteRune(letter)
		}
	}
	text := key.String()
	for _, prefix := range []string{"thanhpho", "tinh"} {
		text = strings.TrimPrefix(text, prefix)
	}
	for _, suffix := range []string{"province", "city", "municipality"} {
		text = strings.TrimSuffix(text, suffix)
	}
	return text
}

// Normalize pre-2025 province names that some map providers still return to
// the 34-province catalog used by checkout; unknown areas need manual input.
func reverseProvince(candidates ...string) string {
	aliases := map[string]string{
		"hagiang": "8", "yenbai": "15", "backan": "19", "vinhphuc": "25", "hoabinh": "25",
		"bacgiang": "24", "haiduong": "31", "thaibinh": "33", "namdinh": "37", "hanam": "37",
		"quangbinh": "44", "thuathienhue": "46", "quangnam": "48", "kontum": "51", "binhdinh": "52",
		"ninhthuan": "56", "phuyen": "66", "daknong": "68", "binhthuan": "68", "binhphuoc": "75",
		"binhduong": "79", "bariavungtau": "79", "longan": "80", "tiengiang": "82", "bentre": "86",
		"travinh": "86", "kiengiang": "91", "soctrang": "92", "haugiang": "92", "baclieu": "96",
	}
	for code, name := range provinceNames {
		aliases[provinceKey(name)] = code
	}
	for _, candidate := range candidates {
		if code := aliases[provinceKey(candidate)]; code != "" {
			return code
		}
	}
	return ""
}
