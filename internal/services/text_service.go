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

func (s *textService) FindByChecksum(ctx context.Context, checksum models.Checksum) (*models.Text, error) {
	text, err := s.persistence.FindByChecksum(ctx, checksum)
	if err != nil {
		return nil, err
	}
	return &text, nil
}

func (s *textService) Update(ctx context.Context, checksum models.Checksum, req dto.UpdateTextRequest) (*models.Text, error) {
	text, err := s.persistence.Update(ctx, checksum, req)
	if err != nil {
		return nil, err
	}
	s.audit.LogAsync(ctx, models.AuditEvent{
		Action:      models.OpTextUpdate,
		EntityType:  "text",
		Checksum:    text.Checksum,
		Details:     map[string]any{"name": text.Name},
		PerformedAt: time.Now(),
	})
	return &text, nil
}

func (s *textService) Delete(ctx context.Context, checksum models.Checksum) (dto.DeleteTextResponse, error) {
	if err := s.persistence.Delete(ctx, checksum); err != nil {
		return dto.DeleteTextResponse{}, err
	}
	response := dto.DeleteTextResponse{Message: "OK", Checksum: checksum}
	s.audit.LogAsync(ctx, models.AuditEvent{
		Action:      models.OpTextDelete,
		EntityType:  "text",
		Checksum:    response.Checksum,
		PerformedAt: time.Now(),
	})
	return response, nil
}