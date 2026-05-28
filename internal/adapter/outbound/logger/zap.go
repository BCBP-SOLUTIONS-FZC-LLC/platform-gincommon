// Package logger provides a zap-backed implementation of port.Logger.
package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ZapLogger adapts go.uber.org/zap to the port.Logger interface.
type ZapLogger struct {
	log *zap.Logger
}

// NewLogger builds a ZapLogger. Development (console-friendly) config is used when env is
// "dev", "development", or "local"; production JSON config is used for all other values.
func NewLogger(env string) (*ZapLogger, error) {
	var cfg zap.Config
	if isDevEnv(env) {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}

	log, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	return &ZapLogger{log: log}, nil
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
	return l.log.Sync()
}

// isDevEnv mirrors config.IsDevEnv without importing the config package.
func isDevEnv(env string) bool {
	return env == "dev" || env == "development" || env == "local"
}
