package metrics_test

import (
	"net/http"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
)

func TestRequestOutcome(t *testing.T) {
	tests := []struct {
		code      int
		wantClass string
	}{
		{http.StatusOK, "success"},
		{http.StatusCreated, "success"},
		{http.StatusMovedPermanently, "success"},
		{http.StatusNotFound, "client_error"},
		{http.StatusUnauthorized, "client_error"},
		{http.StatusInternalServerError, "server_error"},
		{0, "server_error"},
		{199, "success"}, // 1xx is informational, not a failure (101 = WebSocket upgrade)
	}

	for _, tc := range tests {
		ec := metrics.RequestOutcome(tc.code)
		if ec != tc.wantClass {
			t.Errorf("RequestOutcome(%d) = %q, want %q", tc.code, ec, tc.wantClass)
		}
	}
}

func TestHTTPStatusClass(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{200, "2xx"},
		{301, "2xx"},
		{404, "4xx"},
		{500, "5xx"},
		{0, "5xx"},
	}

	for _, tc := range tests {
		if got := metrics.HTTPStatusClass(tc.code); got != tc.want {
			t.Errorf("HTTPStatusClass(%d) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

// 1xx responses (e.g. 101 Switching Protocols for WebSocket upgrades) are not
// failures; counting them as 5xx would page on the platform SLO burn-rate alerts.
func TestStatusClass_InformationalIsSuccess(t *testing.T) {
	for _, code := range []int{100, 101, 103} {
		if got := metrics.HTTPStatusClass(code); got != "2xx" {
			t.Errorf("HTTPStatusClass(%d) = %q, want 2xx", code, got)
		}
		if got := metrics.RequestOutcome(code); got != "success" {
			t.Errorf("RequestOutcome(%d) = %q, want success", code, got)
		}
	}
	if got := metrics.HTTPStatusClass(99); got != "5xx" {
		t.Errorf("HTTPStatusClass(99) = %q, want 5xx for an invalid code", got)
	}
}
