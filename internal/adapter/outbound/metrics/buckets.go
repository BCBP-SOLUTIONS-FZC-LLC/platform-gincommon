package metrics

import "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/obsregistry"

// RequestDurationBuckets are the registry's request_duration preset (seconds),
// tuned for IAM-style workloads: fast authn/authz and token checks (1–50ms),
// policy lookups (50–500ms), occasional heavy paths (1–5s), with a 10s tail
// for timeouts. Used by both HTTP and gRPC duration histograms. It is a copy:
// the registry is the source of truth.
var RequestDurationBuckets = obsregistry.Default().Buckets("request_duration")

// PayloadSizeBuckets are the registry's payload_size preset (bytes): compact
// JSON (JWT claims, policy snippets, role lists) up to the 4 MiB gRPC default
// maximum message size, with a 16 MiB tail for bulk payloads. Used by both
// HTTP and gRPC payload size histograms. It is a copy: the registry is the
// source of truth.
var PayloadSizeBuckets = obsregistry.Default().Buckets("payload_size")
