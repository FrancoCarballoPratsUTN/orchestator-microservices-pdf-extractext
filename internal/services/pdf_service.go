package services

import (
	"bytes"
	"context"
	"errors"
	"time"

	"validationmicroservices-pdf-extractext/internal/checksum"
	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
)

var ErrInvalidPDF = errors.New("invalid pdf: missing %PDF- magic signature")

var pdfSignature = []byte("%PDF-")

type pdfService struct {
	extract ExtractClient
	audit   AuditService
}

func NewPDFService(extractClient ExtractClient, auditService AuditService) PDFService {
	return &pdfService{extract: extractClient, audit: auditService}
}

func (s *pdfService) IngestAndExtract(ctx context.Context, pdfData []byte) (dto.ExtractPDFResponse, error) {
	if !isValidPDF(pdfData) {
		return dto.ExtractPDFResponse{}, ErrInvalidPDF
	}

	document, err := s.extract.Extract(ctx, pdfData)
	if err != nil {
		return dto.ExtractPDFResponse{}, err
	}

	response := dto.ExtractPDFResponse{
		Checksum:  models.Checksum(checksum.Of(document.Text)),
		PageCount: document.PageCount,
		Text:      document.Text,
	}
	s.audit.LogAsync(ctx, models.AuditEvent{
		Action:      models.OpPDFExtract,
		EntityType:  "document",
		Checksum:    response.Checksum,
		Details:     map[string]any{"page_count": response.PageCount},
		PerformedAt: time.Now(),
	})
	return response, nil
}

func isValidPDF(pdfData []byte) bool {
	return bytes.HasPrefix(pdfData, pdfSignature)
}