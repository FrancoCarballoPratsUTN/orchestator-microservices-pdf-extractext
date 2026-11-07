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

// Client es el cliente del MS Extract.
type Client struct {
	http *httpclient.Client
}

// NewClient construye un cliente del MS Extract. Los reintentos son
// responsabilidad de Traefik: el cliente HTTP realiza un único intento.
func NewClient(opts httpclient.Options) *Client {
	return &Client{
		http: httpclient.New(opts),
	}
}

// Extract solicita al MS Extract el texto del PDF recibido.
func (c *Client) Extract(ctx context.Context, pdfData []byte) (dto.ExtractedDocument, error) {
	var document dto.ExtractedDocument
	err := c.http.Do(ctx, http.MethodPost, extractPath, pdfMediaType, pdfData, &document)
	return document, err
}
