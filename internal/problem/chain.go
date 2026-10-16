package problem

import "fmt"

// MaxErrorChain bounds ErrorChainTypes in log entries.
const MaxErrorChain = 8

// ErrorChainTypes lists the dynamic types along err's Unwrap chain (both
// Unwrap() error and Unwrap() []error), outermost first, at most limit entries.
// Type names are code identifiers, never error text, so they are safe to log for an
// error whose message may carry SQL, PII, tokens or secrets.
func ErrorChainTypes(err error, limit int) []string {
	var out []string
	queue := []error{err}
	for len(queue) > 0 && len(out) < limit {
		e := queue[0]
		queue = queue[1:]
		if e == nil {
			continue
		}
		out = append(out, fmt.Sprintf("%T", e))
		switch u := e.(type) {
		case interface{ Unwrap() error }:
			queue = append(queue, u.Unwrap())
		case interface{ Unwrap() []error }:
			queue = append(queue, u.Unwrap()...)
		}
	}
	return out
}
