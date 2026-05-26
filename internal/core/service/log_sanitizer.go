package service

import "strings"

var sensitiveLogKeys = map[string]struct{}{
	"authorization": {},
	"cookie":        {},
	"password":      {},
	"secret":        {},
	"token":         {},
	"access_token":  {},
	"refresh_token": {},
	"id_token":      {},
	"api_key":       {},
	"set-cookie":    {},
	"x-api-key":     {},
	"x-auth-token":  {},
	"credential":    {},
	"private_key":   {},
}

// Sanitize returns a copy of fields safe for structured logging (redacts sensitive keys).
func Sanitize(fields map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(fields))
	for k, v := range fields {
		if isSensitiveLogKey(k) {
			out[k] = "[REDACTED]"
			continue
		}
		out[k] = v
	}
	return out
}

func isSensitiveLogKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	if _, ok := sensitiveLogKeys[k]; ok {
		return true
	}
	return strings.Contains(k, "password") ||
		strings.Contains(k, "secret") ||
		strings.Contains(k, "token") ||
		strings.Contains(k, "credential")
}
