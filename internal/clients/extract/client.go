package extract

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
)

const (
	extractPath  = "/extract"
	pdfMediaType = "application/pdf"
)

type Client struct {
	http *httpclient.Client
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return NewClientWithPolicy(baseURL, timeout, httpclient.DefaultRetryPolicy, nil)
}

func NewClientWithLogger(baseURL string, timeout time.Duration, logger *slog.Logger) *Client {
	return NewClientWithPolicy(baseURL, timeout, httpclient.DefaultRetryPolicy, logger)
}

// NewClientWithPolicy construye el cliente con la policy de retry del servicio.
// El retry de métodos no idempotentes se habilita aquí: /extract es un POST y
// sin AllowNonIdempotent la policy nunca reintentaría la llamada.
func NewClientWithPolicy(baseURL string, timeout time.Duration, policy httpclient.RetryPolicy, logger *slog.Logger) *Client {
	policy.AllowNonIdempotent = true
	return &Client{
		http: httpclient.NewWithPolicy(baseURL, timeout, "", policy, logger),
	}
}

func (c *Client) Extract(ctx context.Context, pdfData []byte) (dto.ExtractedDocument, error) {
	var document dto.ExtractedDocument
	err := c.http.Do(ctx, http.MethodPost, extractPath, pdfMediaType, pdfData, &document)
	return document, err
}
