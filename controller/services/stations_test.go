package services

import (
	"context"
	"math"
	"testing"

	"livesteam_app/config"
	"livesteam_app/internal/shipping"
	"livesteam_app/models"
)

type stationProvider struct {
	rows  []shipping.Station
	wards []shipping.Ward
}

func (*stationProvider) Environment() string { return "staging" }
func (*stationProvider) Provinces(context.Context) ([]shipping.Province, error) {
	return []shipping.Province{{ID: 1, Name: "Hà Nội"}}, nil
}
func (*stationProvider) Districts(context.Context, int) ([]shipping.District, error) {
	return []shipping.District{{ID: 10, Name: "Quận 1", SupportType: 3}, {ID: 11, Name: "Quận 10", SupportType: 3}}, nil
}
func (p *stationProvider) Wards(context.Context, int) ([]shipping.Ward, error) { return p.wards, nil }
func (p *stationProvider) Stations(context.Context, int) (shipping.StationList, error) {
	return shipping.StationList{Data: p.rows, Complete: true}, nil
}
func (*stationProvider) Quote(context.Context, shipping.QuoteRequest) (shipping.Fee, error) {
	panic("station lookup must not quote or ship")
}

func TestNearbyStationsSortAndCanonicalOrigin(t *testing.T) {
	lat, lng, farther := 21.028511, 105.804817, 21.038511
	good := shipping.Station{ID: 1888, Name: "Bưu cục gần", Address: "Địa chỉ GHN", ProvinceName: "Hà Nội", DistrictName: "Quận 1", WardName: "Phường 1", Latitude: &lat, Longitude: &lng}
	far := good
	far.ID, far.Name, far.Latitude = 1889, "Bưu cục xa", &farther
	unknown := good
	unknown.ID, unknown.WardName, unknown.Latitude, unknown.Longitude = 1890, "Phường chưa ánh xạ", nil, nil
	p := &stationProvider{rows: []shipping.Station{far, unknown, good}, wards: []shipping.Ward{{Code: "001", Name: "Phường 1", SupportType: 3}}}
	old := config.ShippingProvider
	config.ShippingProvider = p
	t.Cleanup(func() { config.ShippingProvider = old })
	result, err := NearbyShippingStations(context.Background(), lat, lng, 1, 10)
	if err != nil || len(result.Data) != 3 || result.Data[0].ID != 1888 || result.Data[1].ID != 1889 || result.Data[2].DistanceMeters != nil || result.Data[2].PickupAddress != nil || !result.QuoteOnly {
		t.Fatalf("wrong nearby results: %+v %v", result, err)
	}
	if *result.Data[0].DistanceMeters != 0 || *result.Data[1].DistanceMeters < 1000 {
		t.Fatal("incorrect distance")
	}
	input := models.DeliveryAddress{StationID: 1888, StationName: "forged", CarrierProvinceID: 1, CarrierDistrictID: 10, CarrierWardCode: "forged", Name: "Seller", Phone: "0901234567", AddressLine: "forged"}
	address, err := normalizePickup(context.Background(), input)
	if err != nil || address.AddressLine != good.Address || address.StationName != good.Name || address.CarrierWardCode != "001" || address.Name != "Seller" || *address.Latitude != lat {
		t.Fatalf("unverified station origin: %+v %v", address, err)
	}
	if result.Selected == nil || result.Selected.ID != 1888 {
		t.Fatal("backend did not select the nearest valid station")
	}
	auto := input
	auto.UseNearestStation = true
	auto.Latitude, auto.Longitude = &lat, &lng
	auto.StationID = 9999
	chosen, err := normalizePickup(context.Background(), auto)
	if err != nil || chosen.StationID != 1888 || chosen.Name != "Seller" || chosen.UseNearestStation {
		t.Fatalf("automatic origin was not canonical: %+v %v", chosen, err)
	}
	input.StationID = 1234
	if _, err := normalizePickup(context.Background(), input); err == nil || err.Error() != "shipping_station_invalid" {
		t.Fatalf("accepted forged station: %v", err)
	}
	if _, err := NearbyShippingStations(context.Background(), math.NaN(), lng, 1, 10); err == nil {
		t.Fatal("accepted NaN coordinates")
	}
	if _, err := NearbyShippingStations(context.Background(), lat, lng, 1, 99); err == nil {
		t.Fatal("accepted wrong parent district")
	}
	p.wards = append(p.wards, shipping.Ward{Code: "002", Name: "Phường 1", SupportType: 3})
	if _, err := normalizePickup(context.Background(), models.DeliveryAddress{StationID: 1888, CarrierProvinceID: 1, CarrierDistrictID: 10}); err == nil {
		t.Fatal("ambiguous ward was guessed")
	}
}

func TestGPSCarrierAreaUsesUniqueNamesAndPreservesDigits(t *testing.T) {
	p := &stationProvider{wards: []shipping.Ward{{Code: "001", Name: "Phường 1", SupportType: 3}}}
	old := config.ShippingProvider
	config.ShippingProvider = p
	t.Cleanup(func() { config.ShippingProvider = old })
	location := geocodedLocation{DeliveryAddress: models.DeliveryAddress{ProvinceCode: "1", Ward: "Phường 1"}, provinceHints: []string{"Hà Nội"}, districtHints: []string{"Quận 1"}}
	address := matchGeocodedArea(context.Background(), location)
	if address.CarrierDistrictID != 10 || address.CarrierProvinceID != 1 || address.CarrierWardCode != "001" {
		t.Fatalf("wrong GPS area: %+v", address)
	}
	location.districtHints = []string{"Quận 10"}
	if matchGeocodedArea(context.Background(), location).CarrierDistrictID != 11 {
		t.Fatal("district digits were lost")
	}
	location.districtHints = []string{"Quận không biết"}
	if matchGeocodedArea(context.Background(), location).CarrierDistrictID != 0 {
		t.Fatal("unknown district was guessed")
	}
	location.districtHints = nil
	if matchGeocodedArea(context.Background(), location).CarrierDistrictID != 0 {
		t.Fatal("missing district was guessed")
	}
}
