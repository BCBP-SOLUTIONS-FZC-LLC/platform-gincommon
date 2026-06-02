package metrics_test

import (
	"net/http"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
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
		{199, "server_error"},
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
