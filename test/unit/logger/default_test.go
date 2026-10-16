package logger_test

import (
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/logger"
)

func TestDefault_BuiltOnceAndUsable(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("ENVIRONMENT", "")
	a, err := logger.Default()
	if err != nil || a == nil {
		t.Fatalf("Default() = %v, %v", a, err)
	}
	b, _ := logger.Default()
	if a != b {
		t.Fatal("Default() must return the same logger every call")
	}
	a.Info("default logger works", map[string]any{"k": "v"})
}
