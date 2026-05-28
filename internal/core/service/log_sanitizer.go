package service

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

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
	"jwt":           {},
	"bearer":        {},
	"session":       {},
	"session_id":    {},
	"cert":          {},
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

// SanitizeString strips control characters from s and truncates to 256 bytes at a
// UTF-8 rune boundary to prevent log injection via untrusted string values such as
// panic messages and Authorizer error strings.
func SanitizeString(s string) string {
	const maxLen = 256
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if len(s) <= maxLen {
		return s
	}
	i := maxLen
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i]
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
		strings.Contains(k, "credential") ||
		strings.Contains(k, "session") ||
		strings.Contains(k, "bearer") ||
		// Match _key suffix (signing_key, enc_key, hmac_key) and bare "key",
		// but not arbitrary substrings like "monkey", "hockey", "key_metrics".
		k == "key" || strings.HasSuffix(k, "_key")
}
