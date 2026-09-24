package persistence

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
)

const textsPath = "/texts"

type Client struct {
	http *httpclient.Client
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		http: httpclient.New(baseURL, timeout),
	}
}

func (c *Client) Create(ctx context.Context, payload dto.CreateTextPayload) error {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return err
	}
	return c.http.Do(ctx, http.MethodPost, textsPath, "application/json", &body, nil)
}