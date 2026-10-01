package metrics_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/metrics"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestRequestBodySize(t *testing.T) {
	if got := metrics.RequestBodySize(512); got != 512 {
		t.Fatalf("got %v want 512", got)
	}
	if got := metrics.RequestBodySize(0); got != 0 {
		t.Fatalf("got %v want 0", got)
	}
	if got := metrics.RequestBodySize(-1); got != metrics.UnknownSize {
		t.Fatalf("unknown length: got %v want UnknownSize", got)
	}
}

func TestPayloadSizeBuckets(t *testing.T) {
	b := metrics.PayloadSizeBuckets
	if len(b) < 8 {
		t.Fatalf("expected at least 8 buckets, got %d", len(b))
	}
	for i := 1; i < len(b); i++ {
		if b[i] <= b[i-1] {
			t.Fatalf("buckets must be strictly increasing: %v", b)
		}
	}
}

func TestInit_RegistersSizeHistograms(t *testing.T) {
	if metrics.HttpRequestSizeBytes == nil || metrics.HttpResponseSizeBytes == nil {
		t.Fatal("expected size histograms to be initialized")
	}

	metrics.HttpRequestSizeBytes.WithLabelValues("POST", "/upload", "2xx", "success").Observe(1024)
	metrics.HttpResponseSizeBytes.WithLabelValues("POST", "/upload", "2xx", "success").Observe(256)
}

func TestProtoMessageSize_NonProtoValue_ReturnsZero(t *testing.T) {
	if got := metrics.ProtoMessageSize("not a proto message"); got != 0 {
		t.Fatalf("ProtoMessageSize(string) = %v, want 0", got)
	}
	if got := metrics.ProtoMessageSize(42); got != 0 {
		t.Fatalf("ProtoMessageSize(int) = %v, want 0", got)
	}
}

// TestProtoMessageSize_TypedNilProto ensures the typed-nil guard works.
// A (*emptypb.Empty)(nil) bypasses the plain v == nil check; the reflect guard catches it.
func TestProtoMessageSize_TypedNilProto(t *testing.T) {
	var m *emptypb.Empty // typed nil — implements proto.Message but points to nil
	if got := metrics.ProtoMessageSize(m); got != 0 {
		t.Fatalf("ProtoMessageSize(typed-nil proto) = %v, want 0", got)
	}
}
