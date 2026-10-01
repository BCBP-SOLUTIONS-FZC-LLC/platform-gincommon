// Package logger provides a zap-backed implementation of port.Logger.
package logger

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ZapLogger adapts go.uber.org/zap to the port.Logger interface.
type ZapLogger struct {
	log *zap.Logger
}

// Sampling parameters used when LOG_SAMPLING=true: per second, per message
// and level, the first samplingInitial entries are kept, then every
// samplingThereafter-th.
const (
	samplingTick       = time.Second
	samplingInitial    = 100
	samplingThereafter = 100
)

// NewLogger builds a ZapLogger. Development settings (console encoder, debug
// level, stack traces from warn) apply when env is "dev" or "local";
// production settings (JSON encoder, info level, stack traces from error)
// apply to every other value.
//
// Environment:
//   - LOG_LEVEL (debug, info, warn, error) overrides the env-based level; an
//     invalid value keeps the default and warns once on stderr.
//   - LOG_SAMPLING=true enables Zap sampling. It is off by default: sampling
//     keys on message and level, so it would drop most identical http_request
//     entries (and repeated warn/error entries) above 100 per second.
//   - LOG_FILE (e.g. logs/app.log for local Grafana/Loki via Promtail) also
//     writes JSON to that path, alongside stderr.
//
// The level and sampling apply identically to stderr and LOG_FILE output.
func NewLogger(env string) (*ZapLogger, error) {
	dev := isDevEnv(env)
	level := resolveLevel(dev)

	core := newConsoleCore(dev, level)
	if logFile := os.Getenv("LOG_FILE"); logFile != "" {
		fileCore, err := newFileCore(logFile, level)
		if err != nil {
			return nil, err
		}
		core = zapcore.NewTee(fileCore, core)
	}
	if samplingEnabled() {
		core = zapcore.NewSamplerWithOptions(core, samplingTick, samplingInitial, samplingThereafter)
	}

	opts := []zap.Option{zap.AddCaller(), zap.ErrorOutput(zapcore.Lock(os.Stderr))}
	if dev {
		opts = append(opts, zap.Development(), zap.AddStacktrace(zapcore.WarnLevel))
	} else {
		opts = append(opts, zap.AddStacktrace(zapcore.ErrorLevel))
	}
	return &ZapLogger{log: zap.New(core, opts...)}, nil
}

// resolveLevel returns LOG_LEVEL when valid, else debug (dev) or info.
func resolveLevel(dev bool) zapcore.Level {
	def := zapcore.InfoLevel
	if dev {
		def = zapcore.DebugLevel
	}
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL")))
	switch raw {
	case "":
		return def
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		warnOnce("LOG_LEVEL", raw, def.String()+" (want debug, info, warn or error)")
		return def
	}
}

// samplingEnabled reports LOG_SAMPLING (default false).
func samplingEnabled() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_SAMPLING")))
	switch raw {
	case "true", "1", "yes":
		return true
	case "", "false", "0", "no":
		return false
	default:
		warnOnce("LOG_SAMPLING", raw, "false")
		return false
	}
}

// warned records key=value pairs already reported so an invalid setting is
// warned about once per process, not on every NewLogger call.
var warned sync.Map

func warnOnce(key, value, using string) {
	if _, loaded := warned.LoadOrStore(key+"="+value, struct{}{}); loaded {
		return
	}
	log.Printf("gincommon: invalid %s=%q; using %s", key, value, using)
}

func newFileCore(logFile string, level zapcore.Level) (zapcore.Core, error) {
	dir := filepath.Dir(logFile)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	return zapcore.NewCore(zapcore.NewJSONEncoder(encCfg), zapcore.AddSync(f), level), nil
}

func newConsoleCore(dev bool, level zapcore.Level) zapcore.Core {
	var enc zapcore.Encoder
	if dev {
		enc = zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	} else {
		enc = zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	}
	return zapcore.NewCore(enc, zapcore.Lock(os.Stderr), level)
}

// NewLoggerWithCore builds a ZapLogger for tests or custom zap cores.
func NewLoggerWithCore(core zapcore.Core) *ZapLogger {
	return &ZapLogger{log: zap.New(core)}
}

func toZapFields(fields map[string]interface{}) ([]zap.Field, *[]zap.Field) {
	p := zapFieldPool.Get().(*[]zap.Field)
	zapFields := (*p)[:0]

	for k, v := range fields {
		switch val := v.(type) {
		case string:
			zapFields = append(zapFields, zap.String(k, val))
		case int:
			zapFields = append(zapFields, zap.Int(k, val))
		case int64:
			zapFields = append(zapFields, zap.Int64(k, val))
		case float64:
			zapFields = append(zapFields, zap.Float64(k, val))
		case bool:
			zapFields = append(zapFields, zap.Bool(k, val))
		case error:
			zapFields = append(zapFields, zap.NamedError(k, val))
		case []string:
			zapFields = append(zapFields, zap.Strings(k, val))
		default:
			zapFields = append(zapFields, zap.Any(k, val))
		}
	}

	*p = zapFields
	return zapFields, p
}

func releaseZapFields(p *[]zap.Field) {
	zapFields := *p
	if cap(zapFields) > zapFieldPoolMaxCap {
		*p = make([]zap.Field, 0, zapFieldPoolInitCap)
	} else {
		*p = zapFields[:0]
	}
	zapFieldPool.Put(p)
}

func (l *ZapLogger) write(level func(string, ...zap.Field), msg string, fields map[string]interface{}) {
	zapFields, p := toZapFields(fields)
	level(msg, zapFields...)
	releaseZapFields(p)
}

// Debug logs at debug level with structured fields.
func (l *ZapLogger) Debug(msg string, fields map[string]interface{}) {
	l.write(l.log.Debug, msg, fields)
}

// Info logs at info level with structured fields.
func (l *ZapLogger) Info(msg string, fields map[string]interface{}) {
	l.write(l.log.Info, msg, fields)
}

// Warn logs at warn level with structured fields.
func (l *ZapLogger) Warn(msg string, fields map[string]interface{}) {
	l.write(l.log.Warn, msg, fields)
}

// Error logs at error level with structured fields.
func (l *ZapLogger) Error(msg string, fields map[string]interface{}) {
	l.write(l.log.Error, msg, fields)
}

// Sync flushes any buffered log entries. Call before process exit to avoid
// dropping the last log lines when the OS terminates the process.
func (l *ZapLogger) Sync() error {
	err := l.log.Sync()
	if err == nil {
		return nil
	}
	// stderr sync often fails in tests and non-TTY environments; file sinks still flush.
	msg := err.Error()
	if strings.Contains(msg, "bad file descriptor") || strings.Contains(msg, "inappropriate ioctl") {
		return nil
	}
	return err
}

// isDevEnv mirrors config.IsDevEnv without importing the config package:
// only the registry's developer environments, "local" and "dev", compared
// trimmed and case-insensitively.
func isDevEnv(env string) bool {
	env = strings.ToLower(strings.TrimSpace(env))
	return env == "dev" || env == "local"
}
