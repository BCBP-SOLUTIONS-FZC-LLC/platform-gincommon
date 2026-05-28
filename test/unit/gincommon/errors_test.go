package gincommon_test

import (
	"encoding/json"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
)

func TestErrorResponse_JSONMarshalUnmarshal(t *testing.T) {
	orig := gincommon.ErrorResponse{
		Error:     "not found",
		Status:    404,
		TraceID:   "abc123",
		RequestID: "req-1",
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got gincommon.ErrorResponse
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Error != orig.Error {
		t.Fatalf("Error = %q, want %q", got.Error, orig.Error)
	}
	if got.Status != orig.Status {
		t.Fatalf("Status = %d, want %d", got.Status, orig.Status)
	}
	if got.TraceID != orig.TraceID {
		t.Fatalf("TraceID = %q, want %q", got.TraceID, orig.TraceID)
	}
	if got.RequestID != orig.RequestID {
		t.Fatalf("RequestID = %q, want %q", got.RequestID, orig.RequestID)
	}
}

func TestErrorResponse_JSONFieldNames(t *testing.T) {
	er := gincommon.ErrorResponse{
		Error:     "bad request",
		Status:    400,
		TraceID:   "t1",
		RequestID: "r1",
	}

	b, err := json.Marshal(er)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}

	for _, field := range []string{"error", "status", "trace_id", "request_id"} {
		if _, ok := raw[field]; !ok {
			t.Fatalf("expected JSON field %q not found in %s", field, string(b))
		}
	}
}
