package services

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"validationmicroservices-pdf-extractext/internal/checksum"
	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
)

var ErrInvalidPDF = errors.New("invalid pdf: missing %PDF- magic signature")

// ErrNoExtractableText distingue el PDF escaneado del PDF roto. El Extract
// responde 200 con page_count > 0 y content vacío cuando el PDF tiene páginas pero
// ninguna capa de texto, así que no es un fallo del servicio: es un 422.
var ErrNoExtractableText = errors.New("pdf has no extractable text layer")

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

	if strings.TrimSpace(document.Content) == "" {
		return dto.ExtractPDFResponse{}, ErrNoExtractableText
	}

	// El Extract ya devuelve el texto formateado; no se vuelve a convertir.
	response := dto.ExtractPDFResponse{
		Checksum:  models.Checksum(checksum.Of(document.Content)),
		PageCount: document.PageCount,
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

func isValidPDF(pdfData []byte) bool {
	return bytes.HasPrefix(pdfData, pdfSignature)
}
