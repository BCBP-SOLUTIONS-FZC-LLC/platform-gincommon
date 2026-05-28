package gincommon

// ErrorResponse is the standard error body emitted by platform middleware on 4xx/5xx responses.
// Consuming services can unmarshal this to extract the correlation IDs from error responses.
type ErrorResponse struct {
	Error     string `json:"error"`
	Status    int    `json:"status"`
	TraceID   string `json:"trace_id"`
	RequestID string `json:"request_id"`
}
