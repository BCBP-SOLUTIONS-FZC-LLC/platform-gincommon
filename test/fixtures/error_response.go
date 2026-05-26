package fixtures

import "encoding/json"

// ErrorJSON is the standard JSON body returned by writeJSONError / panic recovery.
type ErrorJSON struct {
	Error     string `json:"error"`
	Status    int    `json:"status"`
	TraceID   string `json:"trace_id"`
	RequestID string `json:"request_id"`
}

// DecodeErrorJSON parses the standard middleware error response body.
func DecodeErrorJSON(body []byte) (ErrorJSON, error) {
	var out ErrorJSON
	if err := json.Unmarshal(body, &out); err != nil {
		return ErrorJSON{}, err
	}
	return out, nil
}
