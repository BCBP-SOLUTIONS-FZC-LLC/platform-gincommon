package fixtures

import "sync"

// MockLogger records the last log call for tests.
type MockLogger struct {
	mu     sync.Mutex
	Level  string
	Msg    string
	Fields map[string]interface{}
}

func (m *MockLogger) record(level, msg string, fields map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Level = level
	m.Msg = msg
	m.Fields = fields
}

// Debug records a debug-level log call.
func (m *MockLogger) Debug(msg string, fields map[string]interface{}) {
	m.record("debug", msg, fields)
}

// Info records an info-level log call.
func (m *MockLogger) Info(msg string, fields map[string]interface{}) {
	m.record("info", msg, fields)
}

// Warn records a warn-level log call.
func (m *MockLogger) Warn(msg string, fields map[string]interface{}) {
	m.record("warn", msg, fields)
}

// Error records an error-level log call.
func (m *MockLogger) Error(msg string, fields map[string]interface{}) {
	m.record("error", msg, fields)
}

// Fatal records a fatal-level log call.
func (m *MockLogger) Fatal(msg string, fields map[string]interface{}) {
	m.record("fatal", msg, fields)
}
