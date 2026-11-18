package routes_test

import (
	"context"
	"fmt"
	"sync/atomic"

	"livesteam_app/internal/shipping"
)

// An in-memory carrier for SQL/HTTP regression. It never opens a carrier
// connection; the GHN client's HTTP contract has separate httptest coverage.
type quoteFixture struct {
	calls          atomic.Int64
	same, other    int64
	beforeQuote    func()
	failure        error
	stationFailure error
	lastQuote      atomic.Pointer[shipping.QuoteRequest]
}

func (*quoteFixture) Environment() string { return "staging" }
func (*quoteFixture) Provinces(context.Context) ([]shipping.Province, error) {
	return []shipping.Province{{ID: 1, Name: "Hà Nội"}, {ID: 79, Name: "Hồ Chí Minh"}}, nil
}
func (*quoteFixture) Districts(_ context.Context, province int) ([]shipping.District, error) {
	return []shipping.District{{ID: province * 100, Name: "Test district", SupportType: 3}}, nil
}
func (*quoteFixture) Wards(_ context.Context, district int) ([]shipping.Ward, error) {
	return []shipping.Ward{{Code: fmt.Sprintf("ward-%d", district), Name: "Phường thử nghiệm", SupportType: 3}}, nil
}
func (f *quoteFixture) Stations(_ context.Context, district int) (shipping.StationList, error) {
	if f.stationFailure != nil {
		return shipping.StationList{}, f.stationFailure
	}
	lat, lng := 21.028511, 105.804817
	return shipping.StationList{Complete: true, Data: []shipping.Station{{ID: 1888, Name: "Bưu cục thử nghiệm", Address: "10 Đường bưu cục", WardName: "Phường thử nghiệm", DistrictName: "Test district", ProvinceName: "Hà Nội", Latitude: &lat, Longitude: &lng}}}, nil
}
func (f *quoteFixture) Quote(_ context.Context, request shipping.QuoteRequest) (shipping.Fee, error) {
	f.calls.Add(1)
	f.lastQuote.Store(&request)
	if f.beforeQuote != nil {
		f.beforeQuote()
	}
	if f.failure != nil {
		return shipping.Fee{}, f.failure
	}
	fee := f.other
	if request.From.DistrictID == request.To.DistrictID {
		fee = f.same
	}
	return shipping.Fee{Total: fee, ServiceFee: fee, ServiceTypeID: 2}, nil
}
