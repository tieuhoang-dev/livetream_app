package services

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"unicode"

	"livesteam_app/config"
	"livesteam_app/internal/shipping"
	"livesteam_app/models"

	"golang.org/x/text/unicode/norm"
)

// Matching is exact after accents and administrative prefixes are removed.
// Digits are preserved and ambiguous matches are never guessed.
func areaKey(value string) string {
	var key strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(value)) {
		if r == 'đ' {
			r = 'd'
		}
		if (unicode.IsLetter(r) || unicode.IsDigit(r)) && !unicode.Is(unicode.Mn, r) {
			key.WriteRune(r)
		}
	}
	text := key.String()
	for _, prefix := range []string{"thanhpho", "thixa", "thitran", "district", "quan", "huyen", "phuong", "ward", "xa"} {
		if strings.HasPrefix(text, prefix) {
			return strings.TrimPrefix(text, prefix)
		}
	}
	return text
}

func validPoint(lat, lng float64) bool {
	return !math.IsNaN(lat) && !math.IsInf(lat, 0) && !math.IsNaN(lng) && !math.IsInf(lng, 0) && lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}
func distanceMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const radians = math.Pi / 180
	dlat, dlng := (lat2-lat1)*radians, (lng2-lng1)*radians
	a := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(lat1*radians)*math.Cos(lat2*radians)*math.Sin(dlng/2)*math.Sin(dlng/2)
	return 6371000 * 2 * math.Asin(math.Sqrt(math.Min(1, math.Max(0, a))))
}

type NearbyStation struct {
	ID             int                     `json:"id"`
	Name           string                  `json:"name"`
	Address        string                  `json:"address"`
	Latitude       *float64                `json:"latitude,omitempty"`
	Longitude      *float64                `json:"longitude,omitempty"`
	DistanceMeters *int                    `json:"distance_meters,omitempty"`
	PickupAddress  *models.DeliveryAddress `json:"pickup_address,omitempty"`
}
type NearbyStationResult struct {
	Selected    *NearbyStation  `json:"selected,omitempty"`
	Data        []NearbyStation `json:"data"`
	District    string          `json:"district"`
	Complete    bool            `json:"complete"`
	Environment string          `json:"environment"`
	QuoteOnly   bool            `json:"quote_only"`
}

func stationArea(ctx context.Context, provider shipping.Provider, provinceID, districtID int) (shipping.Province, shipping.District, []shipping.Ward, error) {
	var province shipping.Province
	var district shipping.District
	if provinceID <= 0 || districtID <= 0 {
		return province, district, nil, errors.New("shipping_address_required")
	}
	provinces, err := provider.Provinces(ctx)
	if err != nil {
		return province, district, nil, err
	}
	for _, row := range provinces {
		if row.ID == provinceID {
			province = row
			break
		}
	}
	if province.ID == 0 || reverseProvince(province.Name) == "" {
		return province, district, nil, errors.New("shipping_address_unavailable")
	}
	districts, err := provider.Districts(ctx, provinceID)
	if err != nil {
		return province, district, nil, err
	}
	for _, row := range districts {
		if row.ID == districtID && (row.SupportType == 1 || row.SupportType == 3) {
			district = row
			break
		}
	}
	if district.ID == 0 {
		return province, district, nil, errors.New("shipping_address_unavailable")
	}
	wards, err := provider.Wards(ctx, districtID)
	return province, district, wards, err
}

func stationPickup(station shipping.Station, province shipping.Province, district shipping.District, wards []shipping.Ward) *models.DeliveryAddress {
	if station.ID <= 0 || strings.TrimSpace(station.Name) == "" || strings.TrimSpace(station.Address) == "" || station.DistrictName == "" || areaKey(station.DistrictName) != areaKey(district.Name) || provinceKey(station.ProvinceName) != provinceKey(province.Name) {
		return nil
	}
	var matches []shipping.Ward
	for _, row := range wards {
		if (row.SupportType == 1 || row.SupportType == 3) && areaKey(row.Name) == areaKey(station.WardName) {
			matches = append(matches, row)
		}
	}
	if len(matches) != 1 {
		return nil
	}
	code := reverseProvince(province.Name)
	address := &models.DeliveryAddress{StationID: station.ID, StationName: limitedAddress(station.Name, 255), LocationMode: "manual", ProvinceCode: code, ProvinceName: provinceNames[code], CarrierProvinceID: province.ID, CarrierDistrictID: district.ID, CarrierWardCode: matches[0].Code, Ward: matches[0].Name, AddressLine: limitedAddress(station.Address, 255)}
	if station.Latitude != nil && station.Longitude != nil && validPoint(*station.Latitude, *station.Longitude) {
		address.Latitude, address.Longitude = station.Latitude, station.Longitude
	}
	return address
}

func NearbyShippingStations(ctx context.Context, latitude, longitude float64, provinceID, districtID int) (NearbyStationResult, error) {
	result := NearbyStationResult{Data: make([]NearbyStation, 0), QuoteOnly: true}
	if !validPoint(latitude, longitude) {
		return result, errors.New("invalid_coordinates")
	}
	provider, err := config.Shipping()
	if err != nil {
		return result, err
	}
	province, district, wards, err := stationArea(ctx, provider, provinceID, districtID)
	if err != nil {
		return result, err
	}
	stations, err := provider.Stations(ctx, districtID)
	if err != nil {
		return result, err
	}
	result.District, result.Complete, result.Environment = district.Name, stations.Complete, provider.Environment()
	for _, row := range stations.Data {
		if row.ID <= 0 || strings.TrimSpace(row.Name) == "" {
			continue
		}
		item := NearbyStation{ID: row.ID, Name: limitedAddress(row.Name, 255), Address: limitedAddress(row.Address, 500), PickupAddress: stationPickup(row, province, district, wards)}
		if row.Latitude != nil && row.Longitude != nil && validPoint(*row.Latitude, *row.Longitude) {
			distance := int(math.Round(distanceMeters(latitude, longitude, *row.Latitude, *row.Longitude)))
			item.Latitude, item.Longitude, item.DistanceMeters = row.Latitude, row.Longitude, &distance
		}
		result.Data = append(result.Data, item)
	}
	sort.SliceStable(result.Data, func(i, j int) bool {
		a, b := result.Data[i].DistanceMeters, result.Data[j].DistanceMeters
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return *a < *b
	})
	for i := range result.Data {
		if result.Data[i].DistanceMeters != nil && result.Data[i].PickupAddress != nil {
			result.Selected = &result.Data[i]
			break
		}
	}
	return result, nil
}

func resolveStationPickup(ctx context.Context, provider shipping.Provider, input models.DeliveryAddress) (models.DeliveryAddress, error) {
	province, district, wards, err := stationArea(ctx, provider, input.CarrierProvinceID, input.CarrierDistrictID)
	if err != nil {
		return input, err
	}
	stations, err := provider.Stations(ctx, district.ID)
	if err != nil {
		return input, err
	}
	for _, station := range stations.Data {
		if station.ID != input.StationID {
			continue
		}
		address := stationPickup(station, province, district, wards)
		if address == nil {
			break
		}
		address.Name, address.Phone = input.Name, input.Phone
		return *address, nil
	}
	return input, errors.New("shipping_station_invalid")
}

func normalizePickup(ctx context.Context, input models.DeliveryAddress) (models.DeliveryAddress, error) {
	if input.UseNearestStation {
		if input.Latitude == nil || input.Longitude == nil {
			return input, errors.New("invalid_coordinates")
		}
		nearby, err := NearbyShippingStations(ctx, *input.Latitude, *input.Longitude, input.CarrierProvinceID, input.CarrierDistrictID)
		if err != nil {
			return input, err
		}
		if nearby.Selected == nil {
			return input, errors.New("shipping_station_unavailable")
		}
		chosen := *nearby.Selected.PickupAddress
		chosen.Name, chosen.Phone = input.Name, input.Phone
		return normalizeDelivery(chosen)
	}
	if input.StationID < 0 {
		return input, errors.New("shipping_station_invalid")
	}
	if input.StationID > 0 {
		provider, err := config.Shipping()
		if err != nil {
			return input, err
		}
		input, err = resolveStationPickup(ctx, provider, input)
		if err != nil {
			return input, err
		}
	} else {
		input.StationName = ""
	}
	return normalizeDelivery(input)
}
