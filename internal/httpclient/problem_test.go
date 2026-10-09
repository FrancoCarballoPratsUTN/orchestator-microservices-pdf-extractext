package httpclient

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestParseProblemDecodesRFC9457Payload(t *testing.T) {
	t.Parallel()

	body := `{
		"type":   "https://example.com/probs/pdf-invalid",
		"title":  "Invalid PDF",
		"status": 400,
		"detail": "Missing %PDF- header"
	}`

	problem, err := ParseProblem(strings.NewReader(body))

	if err != nil {
		t.Fatalf("ParseProblem() unexpected error: %v", err)
	}
	if problem.Type != "https://example.com/probs/pdf-invalid" {
		t.Errorf("Type = %q, want %q", problem.Type, "https://example.com/probs/pdf-invalid")
	}
	if problem.Title != "Invalid PDF" {
		t.Errorf("Title = %q, want %q", problem.Title, "Invalid PDF")
	}
	if problem.Status != 400 {
		t.Errorf("Status = %d, want 400", problem.Status)
	}
	if problem.Detail != "Missing %PDF- header" {
		t.Errorf("Detail = %q, want %q", problem.Detail, "Missing %PDF- header")
	}
}

func TestProblemImplementsError(t *testing.T) {
	t.Parallel()

	problem := Problem{Title: "Bad Gateway", Status: 502, Detail: "Extract unreachable"}
	message := problem.Error()

	if message == "" {
		t.Fatal("Problem.Error() must return a non-empty message")
	}
	if !strings.Contains(message, "502") {
		t.Errorf("Problem.Error() = %q, want it to contain the status 502", message)
	}
	if !strings.Contains(message, "Bad Gateway") {
		t.Errorf("Problem.Error() = %q, want it to contain the title", message)
	}
}

func TestParseProblemErrorsOnInvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := ParseProblem(strings.NewReader("{not json"))

	if err == nil {
		t.Fatal("ParseProblem() expected an error for invalid JSON")
	}
}

func TestProblemContainedInErrorsSlice(t *testing.T) {
	t.Parallel()

	problem := Problem{Title: "Bad Gateway", Status: 502}
	wrapped := errors.Join(problem, errors.New("upstream"))

	if !errors.Is(wrapped, problem) {
		t.Fatal("Expected errors.Is to match the Problem instance")
	}
}

func TestIsNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil is not a 404", err: nil, want: false},
		{name: "404 problem matches", err: Problem{Status: http.StatusNotFound}, want: true},
		{name: "wrapped 404 problem matches", err: fmt.Errorf("lookup: %w", Problem{Status: http.StatusNotFound}), want: true},
		{name: "500 problem does not match", err: Problem{Status: http.StatusInternalServerError}, want: false},
		{name: "401 problem does not match", err: Problem{Status: http.StatusUnauthorized}, want: false},
		{name: "plain error does not match", err: errors.New("boom"), want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsNotFound(tt.err); got != tt.want {
				t.Fatalf("IsNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
