package fixtures

import (
	"encoding/json"
	"strings"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/v2/pkg/problem"
)

// DecodeProblem parses an RFC 9457 problem body written by the middleware.
func DecodeProblem(body []byte) (problem.Problem, error) {
	var out problem.Problem
	if err := json.Unmarshal(body, &out); err != nil {
		return problem.Problem{}, err
	}
	return out, nil
}

// IsProblemType reports whether a problem type URI is the platform type t under any base.
func IsProblemType(uri string, t problem.Type) bool {
	return strings.HasSuffix(uri, "/"+t.ID())
}
