package auditlog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
)

const (
	logsPath        = "/audit/logs"
	jsonMediaType   = "application/json"
)

type Client struct {
	http *httpclient.Client
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{http: httpclient.New(baseURL, timeout)}
}

func (c *Client) Emit(ctx context.Context, event models.AuditEvent) error {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(event); err != nil {
		return fmt.Errorf("encode audit event: %w", err)
	}
	return c.http.Do(ctx, http.MethodPost, logsPath, jsonMediaType, &body, nil)
}

func (c *Client) ListAll(ctx context.Context, skip, limit int) ([]models.AuditLog, error) {
	var logs []models.AuditLog
	path := fmt.Sprintf("%s?skip=%d&limit=%d", logsPath, skip, limit)
	err := c.http.Do(ctx, http.MethodGet, path, "", nil, &logs)
	return logs, err
}

func (c *Client) ListByChecksum(ctx context.Context, checksum models.Checksum) ([]models.AuditLog, error) {
	var logs []models.AuditLog
	path := logsPath + "/checksum/" + url.PathEscape(checksum.String())
	err := c.http.Do(ctx, http.MethodGet, path, "", nil, &logs)
	return logs, err
}