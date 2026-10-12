package httpclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func (p Problem) Error() string {
	return fmt.Sprintf("%s: %s (status %d)", p.Title, p.Detail, p.Status)
}

func ParseProblem(body io.Reader) (Problem, error) {
	var problem Problem
	if err := json.NewDecoder(body).Decode(&problem); err != nil {
		return Problem{}, fmt.Errorf("decode problem details: %w", err)
	}
	return problem, nil
}

// StatusError carries the HTTP status of an upstream error whose body is NOT an
// RFC 9457 Problem (e.g. a Traefik circuit breaker returning a plain-text 503).
// It exists so handlers can still tell the status apart from a transport error.
type StatusError struct {
	Code int
	Path string
}

func (e StatusError) Error() string {
	return fmt.Sprintf("unexpected status %d from %s", e.Code, e.Path)
}

// StatusCode reports the HTTP status carried by err, either as a StatusError
// (non-RFC9457 upstream body) or as a parsed Problem (RFC 9457). Callers use it
// to preserve upstream status codes across the service boundary.
func StatusCode(err error) (int, bool) {
	var statusErr StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Code, true
	}
	var problem Problem
	if errors.As(err, &problem) {
		return problem.Status, true
	}
	return 0, false
}

// IsUnauthorized reports whether err is an upstream 401. Callers use it to tell
// a misconfigured token (a wiring error) apart from transient failures.
func IsUnauthorized(err error) bool {
	var problem Problem
	return errors.As(err, &problem) && problem.Status == http.StatusUnauthorized
}

// IsNotFound reports whether err is an upstream 404. The dedup lookup uses it to
// tell "not stored yet" (a miss) apart from "Persistence is failing" (fail-open).
func IsNotFound(err error) bool {
	var problem Problem
	return errors.As(err, &problem) && problem.Status == http.StatusNotFound
}
