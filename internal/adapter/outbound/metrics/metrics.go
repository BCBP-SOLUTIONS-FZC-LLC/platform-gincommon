package metrics

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/obsregistry"
)

// Prometheus collectors under the Enterprise Platform Observability Standard;
// non-nil after initialization. Names, help, label names and buckets come from
// the Platform Observability Registry (internal/obsregistry/registry.json), and
// every collector carries the {domain, service, environment} const labels.
//
// These exported vars are a read-only view of the active collectors for tests
// and inspection. They are assigned (under initMu) only after every collector
// registered successfully. The recording helpers in record.go never read them:
// they load the active collectorSet, which is published atomically, so
// recording is race-free even while a test re-initializes on an isolated
// registry. Assigning these vars has no effect on recording.
var (
	// HttpRequestsTotal is platform_http_requests_total{method, route, status_class, error_class}.
	HttpRequestsTotal *prometheus.CounterVec
	// HttpRequestDuration is platform_http_request_duration_seconds (request_duration buckets).
	HttpRequestDuration *prometheus.HistogramVec
	// HttpRequestSizeBytes is platform_http_request_size_bytes (payload_size buckets).
	HttpRequestSizeBytes *prometheus.HistogramVec
	// HttpResponseSizeBytes is platform_http_response_size_bytes (payload_size buckets).
	HttpResponseSizeBytes *prometheus.HistogramVec
	// HttpActiveRequests is platform_http_active_requests{route}.
	HttpActiveRequests *prometheus.GaugeVec
	// HttpPanicTotal is platform_http_panics_total{method, route}.
	HttpPanicTotal *prometheus.CounterVec
	// RateLimitedRequestsTotal is platform_http_rate_limited_requests_total{method, route}.
	RateLimitedRequestsTotal *prometheus.CounterVec
	// HttpRequestTimeoutTotal is platform_http_request_timeouts_total{method, route}.
	HttpRequestTimeoutTotal *prometheus.CounterVec

	// GrpcRequestsTotal is platform_grpc_server_handled_total{grpc_method, grpc_code, status_class, error_class}.
	GrpcRequestsTotal *prometheus.CounterVec
	// GrpcRequestDuration is platform_grpc_server_handling_seconds{grpc_method, status_class, error_class}.
	GrpcRequestDuration *prometheus.HistogramVec
	// GrpcRequestSizeBytes is platform_grpc_server_request_size_bytes.
	GrpcRequestSizeBytes *prometheus.HistogramVec
	// GrpcResponseSizeBytes is platform_grpc_server_response_size_bytes.
	GrpcResponseSizeBytes *prometheus.HistogramVec
	// GrpcInflight is platform_grpc_server_inflight_requests{grpc_method}.
	GrpcInflight *prometheus.GaugeVec
	// GrpcRateLimitedRequestsTotal is platform_grpc_rate_limited_requests_total{grpc_method}.
	GrpcRateLimitedRequestsTotal *prometheus.CounterVec
	// GrpcPanicTotal is platform_grpc_panics_total{grpc_method}.
	GrpcPanicTotal *prometheus.CounterVec

	// BuildInfo is platform_build_info{version}, always 1.
	BuildInfo *prometheus.GaugeVec
)

var (
	// active is the published collector set; nil before initialization.
	active atomic.Pointer[collectorSet]
	// initMu serializes initialization (and the exported-var writes) so
	// concurrent Init* calls cannot both register collectors. Readers do not
	// take it: they load active.
	initMu sync.Mutex
)

// collectorSet is one successful initialization: the identity, where the
// collectors were registered, and the collectors themselves.
type collectorSet struct {
	opts        Options
	reg         prometheus.Registerer
	constLabels prometheus.Labels

	buildInfo *prometheus.GaugeVec

	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	httpRequestSize     *prometheus.HistogramVec
	httpResponseSize    *prometheus.HistogramVec
	httpActive          *prometheus.GaugeVec
	httpPanics          *prometheus.CounterVec
	httpRateLimited     *prometheus.CounterVec
	httpTimeouts        *prometheus.CounterVec

	grpcRequestsTotal   *prometheus.CounterVec
	grpcRequestDuration *prometheus.HistogramVec
	grpcRequestSize     *prometheus.HistogramVec
	grpcResponseSize    *prometheus.HistogramVec
	grpcInflight        *prometheus.GaugeVec
	grpcRateLimited     *prometheus.CounterVec
	grpcPanics          *prometheus.CounterVec
}

const defaultVersion = "unknown"

// Options identifies the emitting service for the centrally injected labels.
type Options struct {
	// ServiceName is the service const label (required).
	ServiceName string
	// BuildVersion is the platform_build_info version label; it is trimmed,
	// empty becomes "unknown", and it is sanitised to the registry's version
	// label pattern (see SanitizeVersion) rather than rejected, so CI-style
	// values such as "refs/tags/v1.4.0" or a branch name never fail startup.
	BuildVersion string
	// Domain is the registry domain const label (required).
	Domain string
	// Environment is the environment const label (required; trimmed and
	// lowercased, then checked against the registry vocabulary).
	Environment string
}

// ErrDomainRequired is returned when no domain is configured: every metric
// carries the domain label, so there is nothing valid to emit without one.
var ErrDomainRequired = errors.New("metrics: a domain is required (set Config.Domain or OBSERVABILITY_DOMAIN to a registry domain)")

// ErrIdentityMismatch is returned by InitWithOptions (and makes
// MustInitFromConfig panic) when metrics are already initialized with a
// different domain, service, environment or build version: the process emits
// one identity, so a second, different one would otherwise be silently
// dropped. The wrapping error names the field that differs.
var ErrIdentityMismatch = errors.New("metrics: already initialized with a different identity")

func (o Options) resolve() (Options, error) {
	if o.Domain == "" {
		return o, ErrDomainRequired
	}
	reg := obsregistry.Default()
	env, err := reg.CheckIdentity(o.Domain, o.ServiceName, o.Environment)
	if err != nil {
		return o, fmt.Errorf("metrics: %w", err)
	}
	o.Environment = env
	o.BuildVersion = strings.TrimSpace(o.BuildVersion)
	if o.BuildVersion == "" {
		o.BuildVersion = defaultVersion
	}
	original := o.BuildVersion
	o.BuildVersion = SanitizeVersion(original)
	if o.BuildVersion == "" {
		return o, fmt.Errorf("metrics: build version %q has no characters usable in the version label (set Config.BuildVersion or BUILD_VERSION)", original)
	}
	if o.BuildVersion != original {
		warnSanitizedVersion(original, o.BuildVersion)
	}
	// SanitizeVersion only emits values the registry version pattern allows
	// (pinned against obsregistry's CheckVersion in test/unit/platformmetrics).
	return o, nil
}

// maxVersionLen is the registry version label pattern's length bound
// (^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$).
const maxVersionLen = 64

// SanitizeVersion maps a build version onto the registry's version label
// pattern instead of rejecting it: surrounding space and a leading
// "refs/tags/" or "refs/heads/" are removed, every character outside
// [0-9A-Za-z._+-] becomes '_', leading characters that are not alphanumeric
// are dropped, and the result is truncated to 64 bytes. It returns "" when
// nothing usable remains. "feat/observability-api" becomes
// "feat_observability-api"; "refs/tags/v1.4.0" becomes "v1.4.0".
func SanitizeVersion(version string) string {
	v := strings.TrimSpace(version)
	for _, p := range []string{"refs/tags/", "refs/heads/"} {
		if rest, ok := strings.CutPrefix(v, p); ok {
			v = rest
			break
		}
	}
	var b strings.Builder
	for _, c := range v {
		switch {
		case isAlnum(c):
			b.WriteRune(c)
		case b.Len() == 0:
			// Drop leading non-alphanumerics: the first character must be one.
		case c == '.' || c == '_' || c == '+' || c == '-':
			b.WriteRune(c)
		default:
			b.WriteByte('_')
		}
		if b.Len() == maxVersionLen {
			break
		}
	}
	return b.String()
}

func isAlnum(c rune) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

var (
	versionWarnMu sync.Mutex
	versionWarned = map[string]bool{}
)

// warnSanitizedVersion reports a rewritten build version once per original
// value, on stderr: metrics initialize before any logger is guaranteed.
func warnSanitizedVersion(original, sanitized string) {
	versionWarnMu.Lock()
	defer versionWarnMu.Unlock()
	if versionWarned[original] || len(versionWarned) >= 64 {
		return
	}
	versionWarned[original] = true
	_, _ = fmt.Fprintf(os.Stderr, "gincommon: metrics: build version %q is not a valid version label; using %q\n", original, sanitized)
}

// mismatch returns nil when o and prev are the same resolved identity, else
// ErrIdentityMismatch naming the first differing field.
//
// Domain, service and environment must match exactly. "unknown" (no
// BuildVersion and no BUILD_VERSION) is compatible with any version in either
// order, so a grpccommon.Config{} next to a gincommon.Config{BuildVersion:
// "1.4.0"} works whichever stack is built first: a later "unknown" inherits the
// established version, and a later real version upgrades an established
// "unknown" (see InitWithOptions). Two different real versions are a mismatch.
func (o Options) mismatch(prev Options) error {
	if o.BuildVersion == defaultVersion || prev.BuildVersion == defaultVersion {
		o.BuildVersion = prev.BuildVersion
	}
	for _, f := range []struct{ name, got, want string }{
		{"domain", o.Domain, prev.Domain},
		{"service", o.ServiceName, prev.ServiceName},
		{"environment", o.Environment, prev.Environment},
		{"version", o.BuildVersion, prev.BuildVersion},
	} {
		if f.got != f.want {
			return fmt.Errorf("%w: %s %q, already initialized with %q", ErrIdentityMismatch, f.name, f.got, f.want)
		}
	}
	return nil
}

// ApplyEnvDefaults fills the fields a pkg-level Config left empty: the build
// version from BUILD_VERSION (then "unknown"), the domain from
// OBSERVABILITY_DOMAIN, and the environment from APP_ENV, then ENVIRONMENT —
// the precedence platform-events and platform-pgcommon use.
func ApplyEnvDefaults(opts Options) Options {
	if opts.BuildVersion == "" {
		opts.BuildVersion = config.BuildVersion()
	}
	if opts.Domain == "" {
		opts.Domain = config.ObservabilityDomain()
	}
	if opts.Environment == "" {
		opts.Environment = config.ObservabilityEnvironment()
	}
	return opts
}

// MustInitFromConfig initializes metrics for a pkg-level Config with
// ApplyEnvDefaults. It panics on a missing domain, an identity the registry
// rejects (unknown domain, invalid service, a version with no usable characters, unmappable
// environment), a registration failure, or an identity different from the
// one metrics were already initialized with (ErrIdentityMismatch; a later
// "unknown" version is not a mismatch, see Options.mismatch), like the
// empty-ServiceName check, so misconfiguration fails at startup rather than
// corrupting cross-service aggregation.
func MustInitFromConfig(opts Options) {
	if err := InitWithOptions(ApplyEnvDefaults(opts)); err != nil {
		panic(err.Error())
	}
}

// InitWithOptions validates the identity against the Platform Observability
// Registry and registers the collectors on prometheus.DefaultRegisterer.
//
// The first successful call establishes the process identity. A later call
// with the same resolved identity (an "unknown" version matches any version;
// a real version upgrades an established "unknown") is a no-op (nil); one with a different domain, service,
// environment or version returns ErrIdentityMismatch. A
// validation or registration error leaves metrics uninitialized (nothing stays
// registered), so a corrected call can retry.
func InitWithOptions(opts Options) error {
	opts, err := opts.resolve()
	if err != nil {
		return err
	}
	initMu.Lock()
	defer initMu.Unlock()
	if cs := active.Load(); cs != nil {
		if allowIdentityChange {
			return nil
		}
		if err := opts.mismatch(cs.opts); err != nil {
			return err
		}
		if cs.opts.BuildVersion == defaultVersion && opts.BuildVersion != defaultVersion {
			upgradeVersion(cs, opts.BuildVersion)
		}
		return nil
	}
	return initCollectors(opts, prometheus.DefaultRegisterer)
}

// upgradeVersion replaces an established "unknown" build version with the
// real one a later caller supplied: platform_build_info is relabelled and a new
// collector set carrying the version is published. Callers hold initMu.
func upgradeVersion(cs *collectorSet, version string) {
	next := *cs
	next.opts.BuildVersion = version
	next.buildInfo.Reset()
	next.buildInfo.WithLabelValues(version).Set(1)
	active.Store(&next)
}

// allowIdentityChange (guarded by initMu) restores the pre-ErrIdentityMismatch
// "first caller wins" behaviour for test harnesses; see AllowIdentityChangeForTesting.
var allowIdentityChange bool

// AllowIdentityChangeForTesting makes a later InitWithOptions with a different
// identity a silent no-op (the first identity is kept) instead of returning
// ErrIdentityMismatch, and returns a func restoring the previous setting. The
// test suites build many stacks with different service names in one process;
// this package is internal, so consuming services cannot reach it.
func AllowIdentityChangeForTesting(allow bool) (restore func()) {
	initMu.Lock()
	defer initMu.Unlock()
	prev := allowIdentityChange
	allowIdentityChange = allow
	return func() {
		initMu.Lock()
		defer initMu.Unlock()
		allowIdentityChange = prev
	}
}

// InitWithRegistererOptions initializes the collectors on reg, replacing any
// active ones, so tests can use isolated registries (prometheus.NewRegistry()).
// A later InitWithOptions is checked against this identity. On a registration
// error (e.g. duplicates on reg) nothing stays registered on reg and the
// previously active collectors remain active.
func InitWithRegistererOptions(opts Options, reg prometheus.Registerer) error {
	opts, err := opts.resolve()
	if err != nil {
		return err
	}
	initMu.Lock()
	defer initMu.Unlock()
	return initCollectors(opts, reg)
}

// initCollectors builds every collector, registers them all on reg (rolling
// back on the first failure), and only then publishes them. Callers hold initMu.
func initCollectors(opts Options, reg prometheus.Registerer) error {
	constLabels := prometheus.Labels{
		"domain":      opts.Domain,
		"environment": opts.Environment,
		"service":     opts.ServiceName,
	}
	b := registryBuilder{reg: obsregistry.Default(), constLabels: constLabels}
	cs := &collectorSet{
		opts:        opts,
		reg:         reg,
		constLabels: constLabels,

		buildInfo: b.gaugeVec("platform_build_info"),

		httpRequestsTotal:   b.counterVec("platform_http_requests_total"),
		httpRequestDuration: b.histogramVec("platform_http_request_duration_seconds"),
		httpRequestSize:     b.histogramVec("platform_http_request_size_bytes"),
		httpResponseSize:    b.histogramVec("platform_http_response_size_bytes"),
		httpActive:          b.gaugeVec("platform_http_active_requests"),
		httpPanics:          b.counterVec("platform_http_panics_total"),
		httpRateLimited:     b.counterVec("platform_http_rate_limited_requests_total"),
		httpTimeouts:        b.counterVec("platform_http_request_timeouts_total"),

		grpcRequestsTotal:   b.counterVec("platform_grpc_server_handled_total"),
		grpcRequestDuration: b.histogramVec("platform_grpc_server_handling_seconds"),
		grpcRequestSize:     b.histogramVec("platform_grpc_server_request_size_bytes"),
		grpcResponseSize:    b.histogramVec("platform_grpc_server_response_size_bytes"),
		grpcInflight:        b.gaugeVec("platform_grpc_server_inflight_requests"),
		grpcRateLimited:     b.counterVec("platform_grpc_rate_limited_requests_total"),
		grpcPanics:          b.counterVec("platform_grpc_panics_total"),
	}
	cs.buildInfo.WithLabelValues(opts.BuildVersion).Set(1)

	if err := registerAll(reg, b.built); err != nil {
		return err
	}

	BuildInfo = cs.buildInfo
	HttpRequestsTotal = cs.httpRequestsTotal
	HttpRequestDuration = cs.httpRequestDuration
	HttpRequestSizeBytes = cs.httpRequestSize
	HttpResponseSizeBytes = cs.httpResponseSize
	HttpActiveRequests = cs.httpActive
	HttpPanicTotal = cs.httpPanics
	RateLimitedRequestsTotal = cs.httpRateLimited
	HttpRequestTimeoutTotal = cs.httpTimeouts
	GrpcRequestsTotal = cs.grpcRequestsTotal
	GrpcRequestDuration = cs.grpcRequestDuration
	GrpcRequestSizeBytes = cs.grpcRequestSize
	GrpcResponseSizeBytes = cs.grpcResponseSize
	GrpcInflight = cs.grpcInflight
	GrpcRateLimitedRequestsTotal = cs.grpcRateLimited
	GrpcPanicTotal = cs.grpcPanics
	active.Store(cs)
	return nil
}

// registerAll registers every collector on reg; on the first failure it
// unregisters the ones already registered so a retry starts clean.
func registerAll(reg prometheus.Registerer, cs []prometheus.Collector) error {
	for i, c := range cs {
		if err := reg.Register(c); err != nil {
			for _, done := range cs[:i] {
				reg.Unregister(done)
			}
			return fmt.Errorf("metrics: register collectors: %w", err)
		}
	}
	return nil
}

// registryBuilder builds collectors from registry entries. A missing or
// mistyped entry is a defect in this module (covered by tests) and panics.
type registryBuilder struct {
	reg         *obsregistry.Registry
	constLabels prometheus.Labels
	built       []prometheus.Collector
}

func (b *registryBuilder) entry(name string, t obsregistry.Type) *obsregistry.Metric {
	m, ok := b.reg.Lookup(name)
	if !ok || m.Type != t {
		panic(fmt.Sprintf("metrics: registry has no %s %q", t, name))
	}
	return m
}

func (b *registryBuilder) counterVec(name string) *prometheus.CounterVec {
	m := b.entry(name, obsregistry.Counter)
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: m.Name, Help: m.Help, ConstLabels: b.constLabels}, m.Labels)
	b.built = append(b.built, c)
	return c
}

func (b *registryBuilder) gaugeVec(name string) *prometheus.GaugeVec {
	m := b.entry(name, obsregistry.Gauge)
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: m.Name, Help: m.Help, ConstLabels: b.constLabels}, m.Labels)
	b.built = append(b.built, g)
	return g
}

func (b *registryBuilder) histogramVec(name string) *prometheus.HistogramVec {
	m := b.entry(name, obsregistry.Histogram)
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: m.Name, Help: m.Help, ConstLabels: b.constLabels, Buckets: b.reg.Buckets(m.Buckets),
	}, m.Labels)
	b.built = append(b.built, h)
	return h
}

// Registerer returns the prometheus.Registerer the active collectors were
// registered to, or prometheus.DefaultRegisterer before initialization.
func Registerer() prometheus.Registerer {
	if cs := active.Load(); cs != nil {
		return cs.reg
	}
	return prometheus.DefaultRegisterer
}

// ConstLabels returns a copy of the {domain, environment, service} const labels
// every collector carries (version is only on platform_build_info), so custom
// collectors can match them. Empty before initialization.
func ConstLabels() prometheus.Labels {
	out := prometheus.Labels{}
	if cs := active.Load(); cs != nil {
		maps.Copy(out, cs.constLabels)
	}
	return out
}

// Identity returns the domain and service of the active collectors (empty
// before initialization).
func Identity() (domain, service string) {
	if cs := active.Load(); cs != nil {
		return cs.opts.Domain, cs.opts.ServiceName
	}
	return "", ""
}

// Initialized reports whether the collectors are registered.
func Initialized() bool {
	return active.Load() != nil
}
