package integration

// The gRPC error sanitizer over a real bufconn server running the Full chain: a
// plain handler error never reaches the client as Unknown + err.Error(), and is
// logged as "unclassified error" with types only.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/inbound/label"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/metrics"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/grpccommon"
)

const grpcSecret = "pq: duplicate key users_email_key email=john@example.com"

type entry struct {
	level, msg string
	fields     map[string]any
}

type allLogger struct {
	mu      sync.Mutex
	entries []entry
}

func (l *allLogger) add(level, msg string, f map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry{level, msg, f})
}
func (l *allLogger) Debug(m string, f map[string]any) { l.add("debug", m, f) }
func (l *allLogger) Info(m string, f map[string]any)  { l.add("info", m, f) }
func (l *allLogger) Warn(m string, f map[string]any)  { l.add("warn", m, f) }
func (l *allLogger) Error(m string, f map[string]any) { l.add("error", m, f) }

func (l *allLogger) byMsg(msg string) []entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []entry
	for _, e := range l.entries {
		if e.msg == msg {
			out = append(out, e)
		}
	}
	return out
}

// okStatusErr is a non-nil error whose status claims OK: a handler bug.
type okStatusErr struct{}

func (okStatusErr) Error() string              { return "claims ok " + grpcSecret }
func (okStatusErr) GRPCStatus() *status.Status { return status.New(codes.OK, "") }

func errorsHarness(t *testing.T) (*grpc.ClientConn, *allLogger) {
	t.Helper()
	t.Cleanup(label.ResetGRPCMethods)
	logs := &allLogger{}
	cfg := grpccommon.Config{ServiceName: "grpc-integration", Logger: logs}
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpccommon.FullUnaryInterceptors(cfg)...),
		grpc.ChainStreamInterceptor(grpccommon.FullStreamInterceptors(cfg)...),
	)
	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Errors",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			errMethod("Plain", func() error { return fmt.Errorf("create tender: %w", errors.New(grpcSecret)) }),
			errMethod("Wrapped", func() error {
				return fmt.Errorf("repo %s: %w", grpcSecret, status.Error(codes.NotFound, "Tender 7 was not found."))
			}),
			errMethod("Deadline", func() error { return fmt.Errorf("query %s: %w", grpcSecret, context.DeadlineExceeded) }),
			errMethod("ClaimsOK", func() error { return okStatusErr{} }),
			errMethod("Status", func() error { return status.Error(codes.AlreadyExists, "Tender exists.") }),
		},
		Streams: []grpc.StreamDesc{{
			StreamName:    "Watch",
			ServerStreams: true,
			Handler:       func(any, grpc.ServerStream) error { return errors.New(grpcSecret) },
		}},
	}, struct{}{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, logs
}

func errMethod(name string, fn func() error) grpc.MethodDesc {
	full := "/test.Errors/" + name
	return grpc.MethodDesc{
		MethodName: name,
		Handler: func(_ any, ctx context.Context, dec func(any) error, ic grpc.UnaryServerInterceptor) (any, error) {
			in := new(emptypb.Empty)
			if err := dec(in); err != nil {
				return nil, err
			}
			h := func(context.Context, any) (any, error) { return nil, fn() }
			return ic(ctx, in, &grpc.UnaryServerInfo{FullMethod: full}, h)
		},
	}
}

func call(conn *grpc.ClientConn, method string) error {
	return conn.Invoke(withIdentity(), "/test.Errors/"+method, &emptypb.Empty{}, &emptypb.Empty{})
}

func TestGRPC_ErrorSanitizer(t *testing.T) {
	conn, logs := errorsHarness(t)
	cases := []struct {
		method       string
		code         codes.Code
		msg          string
		unclassified bool
	}{
		{"Plain", codes.Internal, "internal server error", true},
		{"Wrapped", codes.NotFound, "Tender 7 was not found.", false},
		{"Deadline", codes.DeadlineExceeded, "deadline exceeded", false},
		{"ClaimsOK", codes.Internal, "internal server error", true},
		{"Status", codes.AlreadyExists, "Tender exists.", false},
	}
	metricBefore := testutil.ToFloat64(metrics.UnclassifiedErrorsTotal.WithLabelValues("grpc"))
	for _, tc := range cases {
		before := len(logs.byMsg("unclassified error"))
		st := status.Convert(call(conn, tc.method))
		if st.Code() != tc.code || st.Message() != tc.msg {
			t.Errorf("%s: client got %v %q, want %v %q", tc.method, st.Code(), st.Message(), tc.code, tc.msg)
		}
		if got := len(logs.byMsg("unclassified error")) - before; got != map[bool]int{true: 1, false: 0}[tc.unclassified] {
			t.Errorf("%s: %d unclassified entries", tc.method, got)
		}
	}

	if got := testutil.ToFloat64(metrics.UnclassifiedErrorsTotal.WithLabelValues("grpc")) - metricBefore; got != 2 {
		t.Errorf("grpc unclassified delta = %v, want 2 (Plain, ClaimsOK)", got)
	}
	e := logs.byMsg("unclassified error")[0]
	f := e.fields
	if e.level != "error" || f["classified"] != false || f["grpc_method"] != "/test.Errors/Plain" ||
		f["grpc_code"] != "INTERNAL" || f["service"] != "grpc-integration" || f["error_type"] != "*fmt.wrapError" {
		t.Fatalf("entry = %s %v", e.level, f)
	}
	if chain, _ := f["error_chain"].([]string); len(chain) != 2 || chain[1] != "*errors.errorString" {
		t.Fatalf("error_chain = %v", f["error_chain"])
	}
	if rid, _ := f["request_id"].(string); rid == "" {
		t.Fatal("request_id missing")
	}
	// The access log records what the client received.
	matched := 0
	for _, r := range logs.byMsg("grpc_request") {
		if r.fields["method"] == "/test.Errors/Plain" {
			matched++
			if r.fields["grpc_code"] != codes.Internal.String() || r.level != "error" {
				t.Errorf("grpc_request = %s %v, want error-level Internal", r.level, r.fields["grpc_code"])
			}
		}
	}
	if matched != 1 {
		t.Errorf("grpc_request entries for Plain = %d, want 1", matched)
	}
	for _, r := range logs.entries {
		if s := fmt.Sprint(r.fields); strings.Contains(s, "john@example.com") || strings.Contains(s, "pq:") {
			t.Fatalf("%q leaked the error text: %s", r.msg, s)
		}
	}
}

func TestGRPC_ErrorSanitizer_Stream(t *testing.T) {
	conn, logs := errorsHarness(t)
	cs, err := conn.NewStream(withIdentity(), &grpc.StreamDesc{ServerStreams: true}, "/test.Errors/Watch")
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.SendMsg(&emptypb.Empty{}); err != nil {
		t.Fatal(err)
	}
	_ = cs.CloseSend()
	err = cs.RecvMsg(&emptypb.Empty{})
	if err == io.EOF {
		t.Fatal("stream ended without the error")
	}
	if st := status.Convert(err); st.Code() != codes.Internal || st.Message() != "internal server error" {
		t.Fatalf("stream error = %v %q", st.Code(), st.Message())
	}
	if e := logs.byMsg("unclassified error"); len(e) != 1 || e[0].fields["grpc_method"] != "/test.Errors/Watch" {
		t.Fatalf("entries = %+v", e)
	}
}
