package services

import (
	"context"
	"errors"
	"testing"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
)

type stubPersistence struct {
	payload  dto.CreateTextPayload
	err      error
	createCalled bool
}

func (s *stubPersistence) Create(_ context.Context, payload dto.CreateTextPayload) error {
	s.createCalled = true
	s.payload = payload
	return s.err
}

func TestTextServiceCreateDelegatesPayloadToPersistence(t *testing.T) {
	t.Parallel()

	persistence := &stubPersistence{}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	request := dto.CreateTextRequest{
		Text:     "un texto extraido",
		Checksum: models.Checksum("abc123"),
		Name:     "mi documento",
		Metadata: map[string]any{"autor": "joaquin"},
	}

	response, err := service.Create(context.Background(), request)

	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}
	if response.Message != "OK" {
		t.Errorf("Message = %q, want %q", response.Message, "OK")
	}
	if response.Checksum != request.Checksum {
		t.Errorf("Checksum = %q, want %q", response.Checksum, request.Checksum)
	}
	if !persistence.createCalled {
		t.Fatal("persistence.Create was not called")
	}
	if persistence.payload.Text != request.Text {
		t.Errorf("payload.Text = %q, want %q", persistence.payload.Text, request.Text)
	}
	if persistence.payload.Checksum != request.Checksum {
		t.Errorf("payload.Checksum = %q, want %q", persistence.payload.Checksum, request.Checksum)
	}
	if persistence.payload.Name != request.Name {
		t.Errorf("payload.Name = %q, want %q", persistence.payload.Name, request.Name)
	}
	if persistence.payload.Metadata["autor"] != "joaquin" {
		t.Errorf("payload.Metadata = %v, want autor=joaquin", persistence.payload.Metadata)
	}
}

func TestTextServiceCreateRejectsEmptyChecksum(t *testing.T) {
	t.Parallel()

	persistence := &stubPersistence{}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	_, err := service.Create(context.Background(), dto.CreateTextRequest{Text: "sin checksum"})

	if !errors.Is(err, ErrEmptyChecksum) {
		t.Fatalf("error = %v, want ErrEmptyChecksum", err)
	}
	if persistence.createCalled {
		t.Error("persistence.Create was called with an empty checksum")
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0 (checksum vacío no debe auditarse)", len(audit.events))
	}
}

func TestTextServiceCreatePropagatesPersistenceError(t *testing.T) {
	t.Parallel()

	persistErr := errors.New("persistence unreachable")
	persistence := &stubPersistence{err: persistErr}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	_, err := service.Create(context.Background(), dto.CreateTextRequest{Checksum: models.Checksum("abc123")})

	if !errors.Is(err, persistErr) {
		t.Fatalf("error = %v, want %v", err, persistErr)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0 (fallo de persistence no debe auditarse)", len(audit.events))
	}
}

func TestTextServiceCreateEmitsTextCreateAuditEventAfterSuccess(t *testing.T) {
	t.Parallel()

	persistence := &stubPersistence{}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	request := dto.CreateTextRequest{
		Text:     "un texto",
		Checksum: models.Checksum("abc123"),
		Name:     "mi documento",
	}

	response, err := service.Create(context.Background(), request)

	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want %d", len(audit.events), 1)
	}
	event := audit.events[0]
	if event.Action != models.OpTextCreate {
		t.Errorf("Action = %q, want %q", event.Action, models.OpTextCreate)
	}
	if event.EntityType != "text" {
		t.Errorf("EntityType = %q, want %q", event.EntityType, "text")
	}
	if event.Checksum != response.Checksum {
		t.Errorf("Checksum = %q, want %q", event.Checksum, response.Checksum)
	}
	if event.PerformedAt.IsZero() {
		t.Error("PerformedAt is zero")
	}
	details, ok := event.Details.(map[string]any)
	if !ok {
		t.Fatalf("Details = %T, want map[string]any", event.Details)
	}
	if details["name"] != "mi documento" {
		t.Errorf("Details[name] = %v, want %v", details["name"], "mi documento")
	}
}