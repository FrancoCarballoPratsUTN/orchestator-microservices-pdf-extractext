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

	"validationmicroservices-pdf-extractext/internal/logging"
	"validationmicroservices-pdf-extractext/internal/reqctx"
)

type Client struct {
	baseURL   string
	token     string
	budget    time.Duration
	http      *http.Client
	logger    *slog.Logger
	logLimits logging.Limits
}

// Options configures an HTTP client for upstream service calls.
// Los reintentos son responsabilidad de Traefik. El cliente HTTP realiza un único intento.
type Options struct {
	BaseURL string
	Timeout time.Duration
	Token   string
	Logger  *slog.Logger
	Limits  logging.Limits
}

// New builds an HTTP Client with the provided options. A nil Logger disables
// logging. A zero Limits uses the default logging limits.
// Los reintentos son responsabilidad de Traefik. El cliente HTTP realiza un único intento.
func New(opts Options) *Client {
	return &Client{
		baseURL: opts.BaseURL,
		token:   opts.Token,
		budget:  opts.Timeout,
		// The budget is enforced through a context deadline, so the
		// client must NOT impose its own per-request Timeout.
		http:      &http.Client{},
		logger:    opts.Logger,
		logLimits: opts.Limits,
	}
}

// Do performs a single request within a single global time budget.
// Los reintentos son responsabilidad de Traefik.
func (c *Client) Do(ctx context.Context, method, path, contentType string, body []byte, out any) error {
	ctx, cancel := withBudget(ctx, c.budget)
	defer cancel()

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
	if requestID := reqctx.RequestID(ctx); requestID != "" {
		request.Header.Set("X-Request-ID", requestID)
	}
	start := time.Now()
	response, err := c.http.Do(request)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		// Nunca se loguean headers ni el body: solo método, path y upstream.
		// El error tampoco lleva la URL: se registra saneada.
		c.log(ctx, slog.LevelError, "upstream request failed",
			"method", method, "path", path, "upstream", logging.SafeURL(c.baseURL, c.logLimits.ValueLength()),
			"duration_ms", duration, "error", logging.SafeError(err))
		return fmt.Errorf("request to %s: %w", path, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		c.log(ctx, slog.LevelWarn, "upstream returned an error status",
			"method", method, "path", path, "upstream", logging.SafeURL(c.baseURL, c.logLimits.ValueLength()),
			"status", response.StatusCode, "duration_ms", duration)
		if problem, parseErr := ParseProblem(response.Body); parseErr == nil {
			return problem
		}
		return StatusError{Code: response.StatusCode, Path: path}
	}

	c.log(ctx, slog.LevelDebug, "upstream request completed",
		"method", method, "path", path, "upstream", logging.SafeURL(c.baseURL, c.logLimits.ValueLength()),
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
