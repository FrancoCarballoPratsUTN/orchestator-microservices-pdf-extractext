package persistence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
)

const textsPath = "/texts"

type Client struct {
	http *httpclient.Client
}

func NewClient(opts httpclient.Options) *Client {
	return &Client{
		http: httpclient.New(opts),
	}
}

func (c *Client) Create(ctx context.Context, payload dto.CreateTextPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.http.Do(ctx, http.MethodPost, textsPath, "application/json", body, nil)
}

func (c *Client) Update(ctx context.Context, checksum models.Checksum, payload dto.UpdateTextPayload) (models.Text, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return models.Text{}, err
	}
	var text models.Text
	err = c.http.Do(ctx, http.MethodPut, checksumPath(checksum), "application/json", body, &text)
	return text, err
}

func (c *Client) Delete(ctx context.Context, checksum models.Checksum) error {
	return c.http.Do(ctx, http.MethodDelete, checksumPath(checksum), "", nil, nil)
}

func (c *Client) FindByChecksum(ctx context.Context, checksum models.Checksum) (models.Text, error) {
	var text models.Text
	err := c.http.Do(ctx, http.MethodGet, checksumPath(checksum), "", nil, &text)
	return text, err
}

func checksumPath(checksum models.Checksum) string {
	return textsPath + "?checksum=" + url.QueryEscape(checksum.String())
}
