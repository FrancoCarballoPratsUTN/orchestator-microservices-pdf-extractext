package services

import (
	"context"
	"log/slog"
	"time"

	"validationmicroservices-pdf-extractext/internal/checksum"
	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/reqctx"
	"validationmicroservices-pdf-extractext/internal/validation"
)

type pdfService struct {
	extract     ExtractClient
	persistence PersistenceClient
	audit       AuditService
	logger      *slog.Logger
	maxPages    int
}

// NewPDFService construye el servicio de extracción de PDFs.
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

// IngestAndExtract valida el PDF y luego comprueba si sus bytes ya están
// almacenados. El número de páginas siempre proviene de pdfcpu, así que la
// respuesta es idéntica tanto si el texto vino del Extract como de un registro
// almacenado.
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

// findStored devuelve la respuesta cacheada para un checksum. Un 404 es un miss
// y cualquier otro fallo de consulta es fail-open: el llamador extrae igual,
// perdiendo sólo la optimización, nunca la respuesta.
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
		reqctx.Logger(ctx, s.logger).Warn("dedup lookup failed; extracting anyway", "checksum", pdfSum.String(), "error", err)
		return dto.ExtractPDFResponse{}, false
	}
}
