package services

import (
	"bytes"
	"context"
	"errors"

	"validationmicroservices-pdf-extractext/internal/checksum"
	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
)

var ErrInvalidPDF = errors.New("invalid pdf: missing %PDF- magic signature")

var pdfSignature = []byte("%PDF-")

type pdfService struct {
	extract ExtractClient
}

func NewPDFService(extractClient ExtractClient) PDFService {
	return &pdfService{extract: extractClient}
}

func (s *pdfService) IngestAndExtract(ctx context.Context, pdfData []byte) (dto.ExtractPDFResponse, error) {
	if !isValidPDF(pdfData) {
		return dto.ExtractPDFResponse{}, ErrInvalidPDF
	}

	document, err := s.extract.Extract(ctx, pdfData)
	if err != nil {
		return dto.ExtractPDFResponse{}, err
	}

	return dto.ExtractPDFResponse{
		Checksum:  models.Checksum(checksum.Of(document.Text)),
		PageCount: document.PageCount,
		Text:      document.Text,
	}, nil
}

func isValidPDF(pdfData []byte) bool {
	return bytes.HasPrefix(pdfData, pdfSignature)
}