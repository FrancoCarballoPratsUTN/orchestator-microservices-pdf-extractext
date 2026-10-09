package services

import (
	"context"
	"time"

	"validationmicroservices-pdf-extractext/internal/checksum"
	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/validation"
)

type pdfService struct {
	extract  ExtractClient
	audit    AuditService
	maxPages int
}

func NewPDFService(extractClient ExtractClient, auditService AuditService, maxPages int) PDFService {
	return &pdfService{extract: extractClient, audit: auditService, maxPages: maxPages}
}

// IngestAndExtract validates the PDF before spending an Extract call and the
// extracted text before answering. The page count always comes from pdfcpu, so
// the response is identical whether or not a dedup hit ever skips the Extract.
func (s *pdfService) IngestAndExtract(ctx context.Context, pdfData []byte, filename string) (dto.ExtractPDFResponse, error) {
	result, err := validation.PreExtract(validation.Input{
		PDF:      pdfData,
		Filename: filename,
		MaxPages: s.maxPages,
	})
	if err != nil {
		return dto.ExtractPDFResponse{}, err
	}

	document, err := s.extract.Extract(ctx, pdfData)
	if err != nil {
		return dto.ExtractPDFResponse{}, err
	}

	if err := validation.ValidateExtracted(document.Content); err != nil {
		return dto.ExtractPDFResponse{}, err
	}

	// El Extract ya devuelve el texto formateado; no se vuelve a convertir.
	response := dto.ExtractPDFResponse{
		Checksum:  models.Checksum(checksum.Of(document.Content)),
		PageCount: result.PageCount,
		Text:      document.Content,
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
