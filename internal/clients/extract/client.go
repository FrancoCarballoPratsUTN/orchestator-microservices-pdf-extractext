package extract

import (
	"context"
	"net/http"

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

// NewClient builds an Extract client.
// Los reintentos son responsabilidad de Traefik. El cliente HTTP realiza un único intento.
func NewClient(opts httpclient.Options) *Client {
	return &Client{
		http: httpclient.New(opts),
	}
}

func (c *Client) Extract(ctx context.Context, pdfData []byte) (dto.ExtractedDocument, error) {
	var document dto.ExtractedDocument
	err := c.http.Do(ctx, http.MethodPost, extractPath, pdfMediaType, pdfData, &document)
	return document, err
}
