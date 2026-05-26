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
		wantError string
	}{
		{http.StatusOK, "success", "false"},
		{http.StatusCreated, "success", "false"},
		{http.StatusMovedPermanently, "success", "false"},
		{http.StatusNotFound, "client_error", "true"},
		{http.StatusUnauthorized, "client_error", "true"},
		{http.StatusInternalServerError, "server_error", "true"},
		{0, "server_error", "true"},
		{199, "server_error", "true"},
	}

	for _, tc := range tests {
		ec, isErr := metrics.RequestOutcome(tc.code)
		if ec != tc.wantClass || isErr != tc.wantError {
			t.Errorf("RequestOutcome(%d) = (%q, %q), want (%q, %q)",
				tc.code, ec, isErr, tc.wantClass, tc.wantError)
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

func TestErrorClassAndHTTPIsError(t *testing.T) {
	if metrics.ErrorClass(http.StatusOK) != "success" {
		t.Fatalf("ErrorClass(200) = %q", metrics.ErrorClass(http.StatusOK))
	}
	if metrics.HTTPIsError(http.StatusOK) != "false" {
		t.Fatalf("HTTPIsError(200) = %q", metrics.HTTPIsError(http.StatusOK))
	}
	if metrics.HTTPIsError(http.StatusBadRequest) != "true" {
		t.Fatalf("HTTPIsError(400) = %q", metrics.HTTPIsError(http.StatusBadRequest))
	}
}
