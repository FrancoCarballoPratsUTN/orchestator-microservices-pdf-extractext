package persistence

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
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

func (c *Client) Update(ctx context.Context, checksum models.Checksum, payload dto.UpdateTextPayload) (models.Text, error) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return models.Text{}, err
	}
	var text models.Text
	err := c.http.Do(ctx, http.MethodPut, textsPath+"/"+url.PathEscape(checksum.String()), "application/json", &body, &text)
	return text, err
}

func (c *Client) Delete(ctx context.Context, checksum models.Checksum) error {
	return c.http.Do(ctx, http.MethodDelete, textsPath+"/"+url.PathEscape(checksum.String()), "", nil, nil)
}

func (c *Client) FindByChecksum(ctx context.Context, checksum models.Checksum) (models.Text, error) {
	var text models.Text
	err := c.http.Do(ctx, http.MethodGet, textsPath+"/"+url.PathEscape(checksum.String()), "", nil, &text)
	return text, err
}