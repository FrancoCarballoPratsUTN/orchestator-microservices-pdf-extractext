package services

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/reqctx"
)

// maxEmitAttempts es el número de intentos de la capa de aplicación para emitir
// el evento. El cliente AuditLog hace un único intento a nivel transporte
// (Attempts=1) para evitar duplicar el retry.
const maxEmitAttempts = 2

type auditService struct {
	client  AuditLogClient
	logger  *slog.Logger
	timeout time.Duration
	// wg cuenta las emisiones asíncronas en vuelo para que Close pueda drenarlas
	// antes de que el proceso termine.
	wg sync.WaitGroup
}

func NewAuditService(client AuditLogClient, logger *slog.Logger, emitTimeout time.Duration) AuditService {
	return &auditService{client: client, logger: logger, timeout: emitTimeout}
}

func (s *auditService) LogAsync(ctx context.Context, event models.AuditEvent) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.emit(event, context.WithoutCancel(ctx))
	}()
}

// Close bloquea hasta que todas las emisiones en vuelo terminan, o hasta que ctx
// se cancele (lo que ocurra primero). sync.WaitGroup.Wait no acepta contexto, así
// que el deadline se intercepta en una goroutine aparte.
func (s *auditService) Close(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *auditService) emit(event models.AuditEvent, parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= maxEmitAttempts; attempt++ {
		lastErr = s.client.Emit(ctx, event)
		if lastErr == nil {
			return
		}
		if httpclient.IsUnauthorized(lastErr) {
			reqctx.Logger(parent, s.logger).Error("audit emit rejected: check AUDIT_LOG_API_TOKEN", "action", event.Action, "error", lastErr)
			return
		}
	}
	reqctx.Logger(parent, s.logger).Warn("audit emit failed", "action", event.Action, "error", lastErr)
}

func (s *auditService) FetchLogs(ctx context.Context, params dto.AuditQueryParams) (dto.AuditLogsResponse, error) {
	if params.Checksum != "" {
		logs, err := s.client.ListByChecksum(ctx, params.Checksum)
		return dto.AuditLogsResponse{Logs: logs}, err
	}
	logs, err := s.client.ListAll(ctx, params.Skip, params.Limit)
	return dto.AuditLogsResponse{Logs: logs}, err
}
