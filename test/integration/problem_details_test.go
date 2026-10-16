package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/gincommon"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/problem"
)

var orderNotFound = problem.MustType("order", "not-found", "Order not found", http.StatusNotFound, "ORDER_NOT_FOUND")

// problemService is a full DefaultMiddlewares stack served over real HTTP.
func problemService(t *testing.T) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gincommon.DefaultMiddlewares(gincommon.Config{ServiceName: config.TestServiceName()})...)
	r.GET("/orders/:id", func(c *gin.Context) {
		_ = c.Error(problem.NewError(orderNotFound, "Order "+c.Param("id")+" was not found."))
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// A gateway (here httputil.ReverseProxy, which like Envoy and NGINX must not
// intercept upstream errors) forwards the problem unchanged: status, media type,
// body and correlation headers.
func TestProblemDetails_GatewayPassthroughAndClient(t *testing.T) {
	upstream := problemService(t)
	target, _ := url.Parse(upstream.URL)
	gateway := httptest.NewServer(httputil.NewSingleHostReverseProxy(target))
	t.Cleanup(gateway.Close)

	const tp = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	get := func(base string) (*http.Response, []byte) {
		req, _ := http.NewRequest(http.MethodGet, base+"/orders/7", nil)
		req.Header.Set("traceparent", tp)
		req.Header.Set("x-user-id", "u1")
		req.Header.Set("x-tenant-id", "tenant-001")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp, b
	}

	direct, directBody := get(upstream.URL)
	viaGW, gwBody := get(gateway.URL)

	if viaGW.StatusCode != direct.StatusCode || viaGW.StatusCode != http.StatusNotFound {
		t.Fatalf("status direct %d, via gateway %d", direct.StatusCode, viaGW.StatusCode)
	}
	if ct := viaGW.Header.Get("Content-Type"); ct != problem.ContentType {
		t.Fatalf("gateway Content-Type = %q", ct)
	}
	// Bodies differ only in the timestamp (two requests).
	strip := func(b []byte) string { return string(b[:bytes.Index(b, []byte(`"timestamp"`))]) }
	if strip(gwBody) != strip(directBody) {
		t.Fatalf("gateway rewrote the problem:\n%s\n%s", gwBody, directBody)
	}
	if viaGW.Header.Get("X-Trace-Id") != "4bf92f3577b34da6a3ce929d0e0e4736" || viaGW.Header.Get("X-Request-Id") == "" {
		t.Fatalf("correlation headers not forwarded: %v", viaGW.Header)
	}

	// An internal client parses the problem without looking at error strings.
	resp := &http.Response{StatusCode: viaGW.StatusCode, Header: viaGW.Header, Body: io.NopCloser(bytes.NewReader(gwBody))}
	p, err := problem.FromResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != orderNotFound.URI("") || p.ErrorCode != "ORDER_NOT_FOUND" || p.Status != 404 ||
		p.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || p.TenantID != "tenant-001" ||
		p.Instance != "/orders/7" || !strings.HasPrefix(p.Detail, "Order 7") {
		t.Fatalf("problem = %+v", p)
	}
}

// A gateway-generated error (the upstream is down) is itself a problem when the
// gateway builds it with pkg/problem.
func TestProblemDetails_GatewayGeneratedError(t *testing.T) {
	dead, _ := url.Parse("http://127.0.0.1:1")
	proxy := httputil.NewSingleHostReverseProxy(dead)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, _ error) {
		p := problem.NewBuilder(problem.TypeServiceUnavailable).
			Instance(r.URL.Path).
			TraceID(problem.ResolveTraceID(r.Header.Get("traceparent"), r.Header.Get("x-request-id"), "")).
			Build()
		w.Header().Set("Content-Type", problem.ContentType)
		w.WriteHeader(p.Status)
		_ = jsonEncode(w, p)
	}
	gw := httptest.NewServer(proxy)
	t.Cleanup(gw.Close)

	req, _ := http.NewRequest(http.MethodGet, gw.URL+"/orders/1", nil)
	req.Header.Set("x-request-id", "gw-req-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	p, err := problem.FromResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != 503 || p.Type != problem.TypeServiceUnavailable.URI("") || p.TraceID != "gw-req-1" || p.Detail != "" {
		t.Fatalf("gateway problem = %+v", p)
	}
}

func jsonEncode(w io.Writer, v any) error { return json.NewEncoder(w).Encode(v) }
