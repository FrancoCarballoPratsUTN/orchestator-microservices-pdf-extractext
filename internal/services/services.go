package services

import (
	"context"

	"validationmicroservices-pdf-extractext/internal/dto"
)

type PDFService interface {
	IngestAndExtract(ctx context.Context, pdfData []byte) (dto.ExtractPDFResponse, error)
}

type ExtractClient interface {
	Extract(ctx context.Context, pdfData []byte) (dto.ExtractedDocument, error)
}