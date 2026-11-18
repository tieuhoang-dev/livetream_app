package apperrors

import (
	"context"
	"errors"
	"net/http"
)

// Public maps domain errors consistently for HTTP and WebSocket transports.
func Public(err error) (int, string) {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return http.StatusGatewayTimeout, "request_timeout"
	}

	status := http.StatusInternalServerError
	message := err.Error()
	switch message {
	case "invalid_chat_message", "shipping_station_unavailable", "shipping_invalid_parcel", "shipping_address_required", "shipping_address_unavailable", "shipping_station_invalid":
		status = http.StatusBadRequest
	case "shipping_product_incomplete", "shipping_pickup_required", "shipping_pickup_area_required":
		status = http.StatusConflict
	case "shipping_provider_not_configured":
		status = http.StatusServiceUnavailable
	case "shipping_stations_access_required":
		status = http.StatusServiceUnavailable
	case "shipping_unavailable":
		status = http.StatusBadGateway
	case "user_not_found", "order_not_found", "payment_not_found", "product_not_found", "livestream_not_found", "product_not_found_in_livestream":
		status = http.StatusNotFound
	case "forbidden", "cannot_buy_own_product":
		status = http.StatusForbidden
	case "invalid_quantity", "invalid_price", "invalid_action", "invalid_otp", "otp_expired", "user_not_verified", "invalid_product", "empty_update", "invalid_pagination", "invalid_live_comment", "invalid_refund_reason", "invalid_delivery_address", "invalid_payment_method", "invalid_checkout_request", "invalid_shipping_settings":
		status = http.StatusBadRequest
	case "insufficient_stock", "product_already_in_livestream", "product_has_active_orders", "livestream_not_active", "order_not_paid", "order_not_pending", "order_already_paid", "order_already_processed", "product_in_use", "shipping_not_configured", "checkout_changed", "duplicate_request_conflict":
		status = http.StatusConflict
	case "invalid_coordinates", "location_not_supported":
		status = http.StatusBadRequest
	case "live_media_unavailable", "chat_storage_unavailable", "geocoding_unavailable":
		status = http.StatusServiceUnavailable
	case "email_delivery_failed":
		status = http.StatusBadGateway
	default:
		message = "Internal server error"
	}
	return status, message
}
