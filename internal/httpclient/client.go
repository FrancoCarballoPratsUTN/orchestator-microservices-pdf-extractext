package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"validationmicroservices-pdf-extractext/internal/reqctx"
)

type Client struct {
	baseURL string
	token   string
	// budget is the total time allowed for a call, shared by every attempt.
	budget time.Duration
	policy RetryPolicy
	http   *http.Client
	logger *slog.Logger
}

// New builds a Client for a single upstream with no retry. An empty token
// disables the Authorization header, for upstreams that require no
// authentication (Extract).
func New(baseURL string, timeout time.Duration, token string) *Client {
	return newClient(baseURL, timeout, token, DefaultRetryPolicy, nil)
}

// NewWithLogger builds a Client that logs each call and its failures. El logger
// es opcional: New conserva la firma sin logger para no romper a los tests.
func NewWithLogger(baseURL string, timeout time.Duration, token string, logger *slog.Logger) *Client {
	return newClient(baseURL, timeout, token, DefaultRetryPolicy, logger)
}

// NewWithPolicy builds a Client with an explicit retry policy. A nil logger
// disables logging.
func NewWithPolicy(baseURL string, timeout time.Duration, token string, policy RetryPolicy, logger *slog.Logger) *Client {
	return newClient(baseURL, timeout, token, policy, logger)
}

func newClient(baseURL string, timeout time.Duration, token string, policy RetryPolicy, logger *slog.Logger) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		budget:  timeout,
		policy:  policy,
		// The retry loop enforces the budget through a context deadline, so the
		// client must NOT impose its own per-attempt Timeout.
		http:   &http.Client{},
		logger: logger,
	}
}

// Do performs the request, retrying according to the client's policy within a
// single global time budget. The body is passed as bytes so it can be replayed
// across attempts.
func (c *Client) Do(ctx context.Context, method, path, contentType string, body []byte, out any) error {
	attempts := c.policy.attempts()
	if !c.policy.allowsMethod(method) {
		attempts = 1
	}

	ctx, cancel := withBudget(ctx, c.budget)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		lastErr = c.attempt(ctx, method, path, contentType, body, out)
		if lastErr == nil {
			return nil
		}
		if attempt == attempts || !isRetryable(lastErr) {
			return lastErr
		}
		c.log(ctx, slog.LevelWarn, "retrying upstream request",
			"method", method, "path", path, "upstream", c.baseURL,
			"attempt", attempt, "error", lastErr)
		if err := sleepCtx(ctx, c.policy.backoff(attempt)); err != nil {
			return lastErr
		}
	}
	return lastErr
}

// attempt runs a single try and classifies its result.
func (c *Client) attempt(ctx context.Context, method, path, contentType string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request to %s: %w", path, err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	// Correlación end-to-end: se propaga el request_id del contexto a los MS,
	// que ya lo leen y lo devuelven.
	if requestID := reqctx.RequestID(ctx); requestID != "" {
		request.Header.Set("X-Request-ID", requestID)
	}

	start := time.Now()
	response, err := c.http.Do(request)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		// Nunca se loguean headers ni el body: solo método, path y upstream.
		c.log(ctx, slog.LevelError, "upstream request failed",
			"method", method, "path", path, "upstream", c.baseURL,
			"duration_ms", duration, "error", err)
		return transportError{err: fmt.Errorf("request to %s: %w", path, err)}
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		c.log(ctx, slog.LevelWarn, "upstream returned an error status",
			"method", method, "path", path, "upstream", c.baseURL,
			"status", response.StatusCode, "duration_ms", duration)
		if problem, parseErr := ParseProblem(response.Body); parseErr == nil {
			return problem
		}
		return StatusError{Code: response.StatusCode, Path: path}
	}

	c.log(ctx, slog.LevelDebug, "upstream request completed",
		"method", method, "path", path, "upstream", c.baseURL,
		"status", response.StatusCode, "duration_ms", duration)

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response from %s: %w", path, err)
	}
	return nil
}

// withBudget derives a child context bounded by the client's budget. A
// non-positive budget means "no client-imposed deadline".
func withBudget(ctx context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
	if budget <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, budget)
}

func (c *Client) log(ctx context.Context, level slog.Level, msg string, attrs ...any) {
	if c.logger == nil {
		return
	}
	reqctx.Logger(ctx, c.logger).Log(ctx, level, msg, attrs...)
}
