package metrics

import (
	"reflect"

	"google.golang.org/protobuf/proto"
)

// ProtoMessageSize returns the encoded protobuf size in bytes, or 0 for non-proto messages.
// Guards against both untyped nil and typed-nil proto.Message (e.g. (*pb.Response)(nil)
// passed as any) — the latter bypasses a plain `v == nil` check and would panic in proto.Size.
func ProtoMessageSize(v any) float64 {
	if v == nil {
		return 0
	}
	m, ok := v.(proto.Message)
	if !ok {
		return 0
	}
	// Typed-nil: interface is non-nil but the concrete pointer is nil.
	if rv := reflect.ValueOf(m); rv.Kind() == reflect.Pointer && rv.IsNil() {
		return 0
	}
	return float64(proto.Size(m))
}
