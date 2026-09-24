package dto

import "validationmicroservices-pdf-extractext/internal/models"

type CreateTextRequest struct {
	Text     string          `json:"text"`
	Checksum models.Checksum `json:"checksum"`
	Name     string          `json:"name"`
	Metadata map[string]any  `json:"metadata"`
}

type CreateTextResponse struct {
	Message  string          `json:"message"`
	Checksum models.Checksum `json:"checksum"`
}

type CreateTextPayload = CreateTextRequest