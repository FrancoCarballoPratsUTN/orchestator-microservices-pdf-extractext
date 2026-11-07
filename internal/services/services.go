package services

import (
	"context"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
)

// PDFService valida y extrae el texto de un PDF, aplicando dedup contra
// Persistence y registrando la operación en AuditLog.
type PDFService interface {
	IngestAndExtract(ctx context.Context, pdfData []byte, filename string) (dto.ExtractPDFResponse, error)
}

// TextService gestiona el alta, consulta, actualización y borrado de textos en
// Persistence.
type TextService interface {
	Create(ctx context.Context, req dto.CreateTextRequest) (dto.CreateTextResponse, error)
	FindByChecksum(ctx context.Context, checksum models.Checksum) (*models.Text, error)
	Update(ctx context.Context, checksum models.Checksum, req dto.UpdateTextRequest) (*models.Text, error)
	Delete(ctx context.Context, checksum models.Checksum) (dto.DeleteTextResponse, error)
}

// ExtractClient abstrae el MS Extract para poder sustituirlo en los tests.
type ExtractClient interface {
	Extract(ctx context.Context, pdfData []byte) (dto.ExtractedDocument, error)
}

// PersistenceClient abstrae el MS Persistence para poder sustituirlo en los
// tests.
type PersistenceClient interface {
	Create(ctx context.Context, payload dto.CreateTextPayload) error
	FindByChecksum(ctx context.Context, checksum models.Checksum) (models.Text, error)
	Update(ctx context.Context, checksum models.Checksum, payload dto.UpdateTextPayload) (models.Text, error)
	Delete(ctx context.Context, checksum models.Checksum) error
}

// AuditService emite eventos de auditoría de forma asíncrona y consulta el log.
type AuditService interface {
	LogAsync(ctx context.Context, event models.AuditEvent)
	FetchLogs(ctx context.Context, params dto.AuditQueryParams) (dto.AuditLogsResponse, error)
	// Close espera a que las emisiones asíncronas en vuelo terminen, o hasta que
	// ctx se cancele. Lo usa el composition root en el apagado para no descartar
	// eventos de auditoría ya agendados.
	Close(ctx context.Context) error
}

// AuditLogClient abstrae el MS AuditLog para poder sustituirlo en los tests.
type AuditLogClient interface {
	Emit(ctx context.Context, event models.AuditEvent) error
	ListAll(ctx context.Context, skip, limit int) ([]models.AuditLog, error)
	ListByChecksum(ctx context.Context, checksum models.Checksum) ([]models.AuditLog, error)
}
