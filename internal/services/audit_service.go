package services

import (
	"context"
	"log/slog"
	"time"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
)

const maxEmitAttempts = 2

type auditService struct {
	client  AuditLogClient
	logger  *slog.Logger
	timeout time.Duration
}

func NewAuditService(client AuditLogClient, logger *slog.Logger, emitTimeout time.Duration) AuditService {
	return &auditService{client: client, logger: logger, timeout: emitTimeout}
}

func (s *auditService) LogAsync(ctx context.Context, event models.AuditEvent) {
	go s.emit(event, context.WithoutCancel(ctx))
}

func (s *auditService) emit(event models.AuditEvent, parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= maxEmitAttempts; attempt++ {
		if lastErr = s.client.Emit(ctx, event); lastErr == nil {
			return
		}
	}
	s.logger.Warn("audit emit failed", "action", event.Action, "error", lastErr)
}

func (s *auditService) FetchLogs(ctx context.Context, params dto.AuditQueryParams) (dto.AuditLogsResponse, error) {
	if params.Checksum != "" {
		logs, err := s.client.ListByChecksum(ctx, params.Checksum)
		return dto.AuditLogsResponse{Logs: logs}, err
	}
	logs, err := s.client.ListAll(ctx, params.Skip, params.Limit)
	return dto.AuditLogsResponse{Logs: logs}, err
}