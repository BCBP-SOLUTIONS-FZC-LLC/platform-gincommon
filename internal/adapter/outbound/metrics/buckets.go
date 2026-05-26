package metrics

// HTTPRequestDurationBuckets are histogram upper bounds in seconds, tuned for IAM-style
// workloads: fast authn/authz and token checks (1–50ms), policy lookups (50–500ms),
// occasional heavy paths (1–5s), with a 10s tail for timeouts.
var HTTPRequestDurationBuckets = []float64{
	0.001, // 1ms
	0.002,
	0.005,
	0.01,
	0.015,
	0.025,
	0.05,
	0.075,
	0.1,
	0.15,
	0.2,
	0.3,
	0.5,
	0.75,
	1,
	2,
	5,
	10,
}

// HTTPPayloadSizeBuckets are histogram upper bounds in bytes, tuned for IAM APIs:
// compact JSON (JWT claims, policy snippets, role lists) with headroom for bulk policy payloads.
var HTTPPayloadSizeBuckets = []float64{
	128,
	256,
	512,
	1024,
	2048,
	4096,
	8192,
	16384,
	32768,
	65536,
	262144,
	1048576,
}
