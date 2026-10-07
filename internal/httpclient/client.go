package httpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
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

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("request to %s: %w", path, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if problem, parseErr := ParseProblem(response.Body); parseErr == nil {
			return problem
		}
		return fmt.Errorf("unexpected status %d from %s", response.StatusCode, path)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response from %s: %w", path, err)
	}
	return nil
}
