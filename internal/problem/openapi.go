package problem

import _ "embed"

//go:embed openapi.yaml
var openAPIComponents []byte

// StandardResponses maps each status every endpoint declares to its shared
// OpenAPI response component name (#/components/responses/<name>).
func StandardResponses() map[int]string {
	return map[int]string{
		400: "BadRequest",
		401: "Unauthorized",
		403: "Forbidden",
		404: "NotFound",
		409: "Conflict",
		422: "UnprocessableEntity",
		429: "TooManyRequests",
		500: "InternalServerError",
	}
}

// OpenAPIComponents returns the shared OpenAPI 3.1 components document (YAML):
// the ProblemDetails and ValidationError schemas and the standard error responses.
func OpenAPIComponents() []byte {
	return append([]byte(nil), openAPIComponents...)
}
