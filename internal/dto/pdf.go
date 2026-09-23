package dto

import "validationmicroservices-pdf-extractext/internal/models"

type ExtractedPage struct {
	PageNumber int    `json:"page_number"`
	Text       string `json:"text"`
}

type ExtractedDocument struct {
	PageCount  int             `json:"page_count"`
	Pages      []ExtractedPage `json:"pages"`
	Text       string          `json:"text"`
	DurationMs uint64          `json:"duration_ms"`
}

type ExtractPDFResponse struct {
	Checksum  models.Checksum `json:"checksum"`
	PageCount int             `json:"page_count"`
	Text      string          `json:"text"`
}
