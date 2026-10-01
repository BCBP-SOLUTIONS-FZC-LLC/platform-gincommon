package logger_test

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/adapter/outbound/logger"
)

// fileLines builds a logger for env with LOG_FILE set, runs emit, and returns
// the JSON lines written to the file.
func fileLines(t *testing.T, env string, emit func(l *logger.ZapLogger)) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.log")
	t.Setenv("LOG_FILE", path)
	l, err := logger.NewLogger(env)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	emit(l)
	_ = l.Sync()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func countMsg(lines []string, msg string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, `"msg":"`+msg+`"`) {
			n++
		}
	}
	return n
}

func burst(l *logger.ZapLogger) {
	for i := 0; i < 300; i++ {
		l.Info("http_request", map[string]interface{}{"i": i})
	}
}

func TestNewLogger_SamplingDisabledByDefault(t *testing.T) {
	t.Setenv("LOG_SAMPLING", "")
	t.Setenv("LOG_LEVEL", "")
	if n := countMsg(fileLines(t, "prod", burst), "http_request"); n != 300 {
		t.Fatalf("kept %d of 300 identical entries, want all (no sampling)", n)
	}
}

func TestNewLogger_SamplingOptIn(t *testing.T) {
	t.Setenv("LOG_SAMPLING", "true")
	t.Setenv("LOG_LEVEL", "")
	n := countMsg(fileLines(t, "prod", burst), "http_request")
	if n >= 300 || n < 100 {
		t.Fatalf("kept %d of 300 with LOG_SAMPLING=true, want sampled (100..299)", n)
	}
}

func TestNewLogger_InvalidSamplingDisables(t *testing.T) {
	t.Setenv("LOG_SAMPLING", "sometimes")
	t.Setenv("LOG_LEVEL", "")
	if n := countMsg(fileLines(t, "prod", burst), "http_request"); n != 300 {
		t.Fatalf("kept %d of 300 with invalid LOG_SAMPLING, want all", n)
	}
}

func emitAllLevels(l *logger.ZapLogger) {
	l.Debug("lvl_debug", nil)
	l.Info("lvl_info", nil)
	l.Warn("lvl_warn", nil)
	l.Error("lvl_error", nil)
}

func TestNewLogger_LogLevel(t *testing.T) {
	cases := []struct {
		env, level string
		want       []string // messages expected in the file
		absent     []string
	}{
		{"prod", "", []string{"lvl_info", "lvl_warn", "lvl_error"}, []string{"lvl_debug"}},
		{"dev", "", []string{"lvl_debug", "lvl_info"}, nil},
		{"prod", "debug", []string{"lvl_debug"}, nil},
		{"dev", "info", []string{"lvl_info"}, []string{"lvl_debug"}},
		{"prod", "WARN", []string{"lvl_warn", "lvl_error"}, []string{"lvl_info"}},
		{"prod", "warning", []string{"lvl_warn"}, []string{"lvl_info"}},
		{"prod", "error", []string{"lvl_error"}, []string{"lvl_warn"}},
		{"prod", "loud", []string{"lvl_info"}, []string{"lvl_debug"}},    // invalid → default
		{"development", "", []string{"lvl_info"}, []string{"lvl_debug"}}, // not a dev env
		{" Dev ", "", []string{"lvl_debug", "lvl_info"}, nil},            // normalised like APP_ENV
		{"LOCAL", "", []string{"lvl_debug", "lvl_info"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.env+"/"+tc.level, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", tc.level)
			t.Setenv("LOG_SAMPLING", "")
			lines := fileLines(t, tc.env, emitAllLevels)
			for _, m := range tc.want {
				if countMsg(lines, m) != 1 {
					t.Fatalf("%s missing: %v", m, lines)
				}
			}
			for _, m := range tc.absent {
				if countMsg(lines, m) != 0 {
					t.Fatalf("%s should be filtered: %v", m, lines)
				}
			}
		})
	}
}

func TestNewLogger_InvalidLevelWarnsOnce(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	t.Setenv("LOG_FILE", "")
	t.Setenv("LOG_LEVEL", "verbose-once")

	for i := 0; i < 3; i++ {
		if _, err := logger.NewLogger("prod"); err != nil {
			t.Fatalf("NewLogger: %v", err)
		}
	}
	if n := strings.Count(buf.String(), `invalid LOG_LEVEL="verbose-once"`); n != 1 {
		t.Fatalf("warned %d times, want 1: %s", n, buf.String())
	}
}
