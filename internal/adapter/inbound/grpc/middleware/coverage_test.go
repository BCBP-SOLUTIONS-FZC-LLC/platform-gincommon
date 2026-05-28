package middleware

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// ── splitFullMethod ────────────────────────────────────────────────────────────

func TestSplitFullMethod_Normal(t *testing.T) {
	svc, method := splitFullMethod("/pkg.Service/Method")
	if svc != "pkg.Service" || method != "Method" {
		t.Fatalf("got (%q,%q), want (pkg.Service, Method)", svc, method)
	}
}

func TestSplitFullMethod_BareMethod(t *testing.T) {
	svc, method := splitFullMethod("Method")
	if svc != "" || method != "Method" {
		t.Fatalf("got (%q,%q), want ('', Method)", svc, method)
	}
}

func TestSplitFullMethod_EmptyString(t *testing.T) {
	svc, method := splitFullMethod("")
	if svc != "" || method != "unknown" {
		t.Fatalf("got (%q,%q), want ('', 'unknown')", svc, method)
	}
}

func TestSplitFullMethod_SlashOnly(t *testing.T) {
	svc, method := splitFullMethod("/")
	if svc != "" || method != "unknown" {
		t.Fatalf("got (%q,%q), want ('', 'unknown')", svc, method)
	}
}

func TestSplitFullMethod_DoubleSlash(t *testing.T) {
	svc, method := splitFullMethod("//pkg.Service/Method")
	if svc != "pkg.Service" || method != "Method" {
		t.Fatalf("got (%q,%q), want (pkg.Service, Method)", svc, method)
	}
}

// ── metadataCarrier ────────────────────────────────────────────────────────────

func TestMetadataCarrier_Get_KeyPresent(t *testing.T) {
	md := metadata.MD{"traceparent": []string{"00-abc-def-01"}}
	mc := metadataCarrier(md)
	got := mc.Get("traceparent")
	if got != "00-abc-def-01" {
		t.Fatalf("Get = %q, want %q", got, "00-abc-def-01")
	}
}

func TestMetadataCarrier_Get_KeyAbsent(t *testing.T) {
	md := metadata.MD{}
	mc := metadataCarrier(md)
	got := mc.Get("traceparent")
	if got != "" {
		t.Fatalf("Get = %q, want empty", got)
	}
}

func TestMetadataCarrier_Set(t *testing.T) {
	md := metadata.MD{}
	mc := metadataCarrier(md)
	mc.Set("x-custom", "value1")
	if got := mc.Get("x-custom"); got != "value1" {
		t.Fatalf("after Set, Get = %q, want %q", got, "value1")
	}
}

func TestMetadataCarrier_Keys(t *testing.T) {
	md := metadata.MD{"a": []string{"1"}, "b": []string{"2"}}
	mc := metadataCarrier(md)
	keys := mc.Keys()
	if len(keys) != 2 {
		t.Fatalf("len(Keys) = %d, want 2", len(keys))
	}
}

// ── clientIPFromContext ────────────────────────────────────────────────────────

func TestClientIPFromContext_WithAddr(t *testing.T) {
	addr := &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 50001}
	ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: addr})
	got := clientIPFromContext(ctx)
	if got == "" {
		t.Fatal("expected non-empty IP, got empty string")
	}
}

func TestClientIPFromContext_NoPeer(t *testing.T) {
	got := clientIPFromContext(context.Background())
	if got != "" {
		t.Fatalf("expected empty IP without peer, got %q", got)
	}
}
