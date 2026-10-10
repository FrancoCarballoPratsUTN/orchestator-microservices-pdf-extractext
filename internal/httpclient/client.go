package httpclient

import (
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
	http    *http.Client
	logger  *slog.Logger
}

// New builds a Client for a single upstream. An empty token disables the
// Authorization header, for upstreams that require no authentication (Extract).
func New(baseURL string, timeout time.Duration, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: timeout},
	}
}

// NewWithLogger builds a Client that logs each call and its failures. El logger
// es opcional: New conserva la firma sin logger para no romper a los tests.
func NewWithLogger(baseURL string, timeout time.Duration, token string, logger *slog.Logger) *Client {
	client := New(baseURL, timeout, token)
	client.logger = logger
	return client
}

func (c *Client) Do(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build request to %s: %w", path, err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	start := time.Now()
	response, err := c.http.Do(request)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		// Nunca se loguean headers ni el body: solo método, path y upstream.
		c.log(ctx, slog.LevelError, "upstream request failed",
			"method", method, "path", path, "upstream", c.baseURL,
			"duration_ms", duration, "error", err)
		return fmt.Errorf("request to %s: %w", path, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		c.log(ctx, slog.LevelWarn, "upstream returned an error status",
			"method", method, "path", path, "upstream", c.baseURL,
			"status", response.StatusCode, "duration_ms", duration)
		if problem, parseErr := ParseProblem(response.Body); parseErr == nil {
			return problem
		}
		return fmt.Errorf("unexpected status %d from %s", response.StatusCode, path)
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

func (c *Client) log(ctx context.Context, level slog.Level, msg string, attrs ...any) {
	if c.logger == nil {
		return
	}
	reqctx.Logger(ctx, c.logger).Log(ctx, level, msg, attrs...)
}
