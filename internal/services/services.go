package services

import (
	"context"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
)

type PDFService interface {
	IngestAndExtract(ctx context.Context, pdfData []byte) (dto.ExtractPDFResponse, error)
}

type TextService interface {
	Create(ctx context.Context, req dto.CreateTextRequest) (dto.CreateTextResponse, error)
	FindByChecksum(ctx context.Context, checksum models.Checksum) (*models.Text, error)
	Update(ctx context.Context, checksum models.Checksum, req dto.UpdateTextRequest) (*models.Text, error)
	Delete(ctx context.Context, checksum models.Checksum) (dto.DeleteTextResponse, error)
}

type ExtractClient interface {
	Extract(ctx context.Context, pdfData []byte) (dto.ExtractedDocument, error)
}

type PersistenceClient interface {
	Create(ctx context.Context, payload dto.CreateTextPayload) error
	FindByChecksum(ctx context.Context, checksum models.Checksum) (models.Text, error)
	Update(ctx context.Context, checksum models.Checksum, payload dto.UpdateTextPayload) (models.Text, error)
	Delete(ctx context.Context, checksum models.Checksum) error
}

type AuditService interface {
	LogAsync(ctx context.Context, event models.AuditEvent)
	FetchLogs(ctx context.Context, params dto.AuditQueryParams) (dto.AuditLogsResponse, error)
}

type AuditLogClient interface {
	Emit(ctx context.Context, event models.AuditEvent) error
	ListAll(ctx context.Context, skip, limit int) ([]models.AuditLog, error)
	ListByChecksum(ctx context.Context, checksum models.Checksum) ([]models.AuditLog, error)
}