package smoke

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/logger"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/server"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/fixtures"
	_ "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/test/testenv"
)

func startTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	log, err := logger.NewLogger("dev")
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	return httptest.NewServer(server.NewRouter(log, "smoke-test"))
}

func TestSmoke_Health_InProcess(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/health", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestSmoke_Health_PublicWithoutAuthInProcess(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestSmoke_Metrics_InProcess(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHealthEndpoint_LiveServer(t *testing.T) {
	baseURL := config.SmokeBaseURL()

	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest(http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	fixtures.ApplyHeaders(req, fixtures.DefaultHeaders())

	resp, err := client.Do(req)
	if err != nil {
		t.Skipf("smoke test skipped: no service at %s (%v). Start the app or set SMOKE_BASE_URL in .env.", baseURL, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected %d got %d", http.StatusOK, resp.StatusCode)
	}
}
