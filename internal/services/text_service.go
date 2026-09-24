package services

import (
	"context"
	"errors"
	"time"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
)

var ErrEmptyChecksum = errors.New("text checksum is required")

type textService struct {
	persistence PersistenceClient
	audit       AuditService
}

func NewTextService(persistenceClient PersistenceClient, auditService AuditService) TextService {
	return &textService{persistence: persistenceClient, audit: auditService}
}

func (s *textService) Create(ctx context.Context, req dto.CreateTextRequest) (dto.CreateTextResponse, error) {
	if req.Checksum == "" {
		return dto.CreateTextResponse{}, ErrEmptyChecksum
	}

	if err := s.persistence.Create(ctx, req); err != nil {
		return dto.CreateTextResponse{}, err
	}

	response := dto.CreateTextResponse{Message: "OK", Checksum: req.Checksum}
	s.audit.LogAsync(ctx, models.AuditEvent{
		Action:      models.OpTextCreate,
		EntityType:  "text",
		Checksum:    response.Checksum,
		Details:     map[string]any{"name": req.Name},
		PerformedAt: time.Now(),
	})
	return response, nil
}