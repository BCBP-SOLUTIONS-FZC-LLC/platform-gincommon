package problem

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

// MaxRequestIDLength is the longest x-request-id accepted as a traceId.
const MaxRequestIDLength = 128

// W3C Trace Context traceparent: version-traceid-parentid-flags, lower-case hex.
// Version ff is invalid; version 00 has exactly four fields; a later version may
// append more after a "-". The same rule as platform-pgcommon-py.
var (
	traceparentPattern = regexp.MustCompile(`^([0-9a-f]{2})-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})(-.*)?$`)
	requestIDPattern   = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

const (
	zeroTraceID  = "00000000000000000000000000000000"
	zeroParentID = "0000000000000000"
)

// TraceIDFromTraceparent is the 32-hex trace-id of a valid W3C traceparent, or ""
// (absent, malformed, upper-case, version ff, an all-zero trace-id or parent-id, or
// version 00 with extra fields).
func TraceIDFromTraceparent(v string) string {
	m := traceparentPattern.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil || m[1] == "ff" || (m[1] == "00" && m[5] != "") {
		return ""
	}
	if m[2] == zeroTraceID || m[3] == zeroParentID {
		return ""
	}
	return m[2]
}

// SanitizeRequestID is v without surrounding whitespace when it is 1 to 128
// characters of [A-Za-z0-9._:-], else "". The value is reflected into the response
// and the log, so anything else is ignored rather than truncated or escaped.
func SanitizeRequestID(v string) string {
	v = strings.TrimSpace(v)
	if !requestIDPattern.MatchString(v) {
		return ""
	}
	return v
}

// ResolveTraceID is the traceId of a problem, the first usable of: a valid
// traceparent's trace-id; a sanitized x-request-id; spanTraceID (the active
// OpenTelemetry span's trace id, if any); a new random 32-hex id. It is the order
// platform-pgcommon-py uses, so Go and Python services answer the same traceId.
func ResolveTraceID(traceparent, requestID, spanTraceID string) string {
	if id := TraceIDFromTraceparent(traceparent); id != "" {
		return id
	}
	if id := SanitizeRequestID(requestID); id != "" {
		return id
	}
	if spanTraceID != "" && spanTraceID != zeroTraceID {
		return spanTraceID
	}
	return NewTraceID()
}

// NewTraceID is a random 32-hex id, shaped like a W3C trace-id.
func NewTraceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error (Go 1.24+).
	return hex.EncodeToString(b[:])
}
