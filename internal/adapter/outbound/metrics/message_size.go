package metrics

import "google.golang.org/protobuf/proto"

// ProtoMessageSize returns the encoded protobuf size in bytes, or 0 for non-proto messages.
func ProtoMessageSize(v any) float64 {
	if v == nil {
		return 0
	}
	if m, ok := v.(proto.Message); ok {
		return float64(proto.Size(m))
	}
	return 0
}
