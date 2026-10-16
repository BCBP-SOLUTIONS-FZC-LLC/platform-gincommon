package logger_test

import (
	"errors"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/internal/adapter/outbound/logger"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestZapLogger_TypedFields(t *testing.T) {
	core, recorded := observer.New(zapcore.DebugLevel)
	log := logger.NewLoggerWithCore(core)

	errVal := errors.New("boom")
	log.Info("typed", map[string]interface{}{
		"s":   "text",
		"i":   42,
		"i64": int64(99),
		"f":   3.14,
		"b":   true,
		"e":   errVal,
		"ss":  []string{"a", "b"},
	})

	entries := recorded.AllUntimed()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Message != "typed" {
		t.Fatalf("message = %q", entries[0].Message)
	}

	got := observerFieldMap(entries[0].Context)

	if got["s"] != "text" {
		t.Errorf("s = %#v", got["s"])
	}
	if got["i"] != int64(42) {
		t.Errorf("i = %#v", got["i"])
	}
	if got["i64"] != int64(99) {
		t.Errorf("i64 = %#v", got["i64"])
	}
	if got["f"] != 3.14 {
		t.Errorf("f = %#v", got["f"])
	}
	if got["b"] != true {
		t.Errorf("b = %#v", got["b"])
	}
	if got["e"] != "boom" {
		t.Errorf("e = %#v", got["e"])
	}
	ss, ok := got["ss"].([]interface{})
	if !ok || len(ss) != 2 || ss[0] != "a" || ss[1] != "b" {
		t.Errorf("ss = %#v", got["ss"])
	}
}

func TestZapLogger_DefaultTypeUsesAny(t *testing.T) {
	core, recorded := observer.New(zapcore.DebugLevel)
	log := logger.NewLoggerWithCore(core)

	log.Info("fallback", map[string]interface{}{
		"def": map[string]int{"nested": 1},
	})

	got := observerFieldMap(recorded.AllUntimed()[0].Context)
	def, ok := got["def"].(map[string]int)
	if !ok || def["nested"] != 1 {
		t.Fatalf("def = %#v", got["def"])
	}
}

func TestZapLogger_NilAndEmptyFields(t *testing.T) {
	core, recorded := observer.New(zapcore.DebugLevel)
	log := logger.NewLoggerWithCore(core)

	log.Debug("nil_map", nil)
	log.Info("empty_map", map[string]interface{}{})

	if len(recorded.AllUntimed()) != 2 {
		t.Fatalf("entries = %d, want 2", len(recorded.AllUntimed()))
	}
}

func TestZapLogger_PoolReuseUnderConcurrency(t *testing.T) {
	core, recorded := observer.New(zapcore.DebugLevel)
	log := logger.NewLoggerWithCore(core)

	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func(n int) {
			log.Info("concurrent", map[string]interface{}{
				"n": n,
				"s": "ok",
			})
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 50; i++ {
		<-done
	}

	if len(recorded.AllUntimed()) != 50 {
		t.Fatalf("entries = %d, want 50", len(recorded.AllUntimed()))
	}
}

func observerFieldMap(ctx []zapcore.Field) map[string]interface{} {
	out := make(map[string]interface{}, len(ctx))
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range ctx {
		f.AddTo(enc)
	}
	for k, v := range enc.Fields {
		out[k] = v
	}
	return out
}
