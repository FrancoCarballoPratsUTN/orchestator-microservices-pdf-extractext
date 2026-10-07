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

// IsUnauthorized reports whether err is an upstream 401. Callers use it to tell
// a misconfigured token (a wiring error) apart from transient failures.
func IsUnauthorized(err error) bool {
	var problem Problem
	return errors.As(err, &problem) && problem.Status == http.StatusUnauthorized
}
