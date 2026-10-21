package problem

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

// MaxClientBodyBytes bounds how much of an error response FromResponse reads.
const MaxClientBodyBytes = 1 << 20

// ErrNotProblem is returned by FromResponse for a response that is not an
// application/problem+json body.
var ErrNotProblem = errors.New("problem: response is not application/problem+json")

// IsProblemContentType reports whether a Content-Type header value is
// application/problem+json (parameters such as charset are ignored).
func IsProblemContentType(v string) bool {
	mt, _, err := mime.ParseMediaType(v)
	return err == nil && mt == ContentType
}

// Decode parses a problem body. Members it does not recognise are kept in
// Problem.Extensions (numbers as json.Number), so a client can read a server's
// extension members (see Problem.Extension), and a newer server's extensions do not
// break older clients.
func Decode(r io.Reader) (*Problem, error) {
	var p Problem
	if err := json.NewDecoder(io.LimitReader(r, MaxClientBodyBytes)).Decode(&p); err != nil {
		return nil, fmt.Errorf("problem: decode: %w", err)
	}
	return &p, nil
}

// FromResponse parses resp's problem body for internal clients and SDKs: branch on
// Status, Type and ErrorCode, never on the text of Title or Detail. It returns
// ErrNotProblem when resp is not application/problem+json. It reads (up to
// MaxClientBodyBytes of) resp.Body but does not close it. When the body omits
// status, the response status is used.
func FromResponse(resp *http.Response) (*Problem, error) {
	if resp == nil || !IsProblemContentType(resp.Header.Get("Content-Type")) {
		return nil, ErrNotProblem
	}
	p, err := Decode(resp.Body)
	if err != nil {
		return nil, err
	}
	if p.Status == 0 {
		p.Status = resp.StatusCode
	}
	return p, nil
}
