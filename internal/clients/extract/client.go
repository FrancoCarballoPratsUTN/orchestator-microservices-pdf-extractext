package extract

import (
	"bytes"
	"context"
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
	return &Client{
		http: httpclient.New(baseURL, timeout),
	}
}

func (c *Client) Extract(ctx context.Context, pdfData []byte) (dto.ExtractedDocument, error) {
	var document dto.ExtractedDocument
	err := c.http.Do(ctx, http.MethodPost, extractPath, pdfMediaType, bytes.NewReader(pdfData), &document)
	return document, err
}
