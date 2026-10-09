package services

import (
	"context"
	"log/slog"
	"time"

	"validationmicroservices-pdf-extractext/internal/checksum"
	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/validation"
)

type pdfService struct {
	extract     ExtractClient
	persistence PersistenceClient
	audit       AuditService
	logger      *slog.Logger
	maxPages    int
}

func NewPDFService(
	extractClient ExtractClient,
	persistenceClient PersistenceClient,
	auditService AuditService,
	logger *slog.Logger,
	maxPages int,
) PDFService {
	return &pdfService{
		extract:     extractClient,
		persistence: persistenceClient,
		audit:       auditService,
		logger:      logger,
		maxPages:    maxPages,
	}
}

// IngestAndExtract validates the PDF, then checks whether its bytes are already
// stored. The page count always comes from pdfcpu, so the response is identical
// whether the text came from Extract or from a stored record.
func (s *pdfService) IngestAndExtract(ctx context.Context, pdfData []byte, filename string) (dto.ExtractPDFResponse, error) {
	result, err := validation.PreExtract(validation.Input{
		PDF:      pdfData,
		Filename: filename,
		MaxPages: s.maxPages,
	})
	if err != nil {
		return dto.ExtractPDFResponse{}, err
	}

	pdfSum := models.Checksum(checksum.OfBytes(pdfData))

	if stored, ok := s.findStored(ctx, pdfSum, result.PageCount); ok {
		return stored, nil
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
		Checksum:  pdfSum,
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

// findStored returns the cached response for a checksum. A 404 is a miss and any
// other lookup failure is fail-open: the caller extracts anyway, losing only the
// optimization, never the response.
func (s *pdfService) findStored(ctx context.Context, pdfSum models.Checksum, pageCount int) (dto.ExtractPDFResponse, bool) {
	stored, err := s.persistence.FindByChecksum(ctx, pdfSum)
	switch {
	case err == nil:
		return dto.ExtractPDFResponse{
			Checksum:  pdfSum,
			PageCount: pageCount,
			Text:      stored.Text,
		}, true
	case httpclient.IsNotFound(err):
		return dto.ExtractPDFResponse{}, false
	default:
		s.logger.Warn("dedup lookup failed; extracting anyway", "checksum", pdfSum.String(), "error", err)
		return dto.ExtractPDFResponse{}, false
	}
}
