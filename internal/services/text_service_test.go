package services

import (
	"context"
	"errors"
	"testing"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
)

type stubPersistence struct {
	payload        dto.CreateTextPayload
	err            error
	createCalled   bool
	updateError    error
	deleteError    error
	findError      error
	updatedText    models.Text
	foundText      models.Text
	updateCalled   bool
	deleteCalled   bool
	findCalled     bool
	updateChecksum models.Checksum
	deleteChecksum models.Checksum
	findChecksum   models.Checksum
	updatePayload  dto.UpdateTextPayload
}

func (s *stubPersistence) Create(_ context.Context, payload dto.CreateTextPayload) error {
	s.createCalled = true
	s.payload = payload
	return s.err
}

func (s *stubPersistence) Update(_ context.Context, checksum models.Checksum, payload dto.UpdateTextPayload) (models.Text, error) {
	s.updateCalled = true
	s.updateChecksum = checksum
	s.updatePayload = payload
	return s.updatedText, s.updateError
}

func (s *stubPersistence) Delete(_ context.Context, checksum models.Checksum) error {
	s.deleteCalled = true
	s.deleteChecksum = checksum
	return s.deleteError
}

func (s *stubPersistence) FindByChecksum(_ context.Context, checksum models.Checksum) (models.Text, error) {
	s.findCalled = true
	s.findChecksum = checksum
	return s.foundText, s.findError
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

func TestTextServiceUpdateDelegatesNameAndMetadataOnly(t *testing.T) {
	t.Parallel()

	persistence := &stubPersistence{
		updatedText: models.Text{Checksum: models.Checksum("abc123"), Text: "un texto", Name: "nuevo nombre"},
	}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	request := dto.UpdateTextRequest{Name: "nuevo nombre", Metadata: map[string]any{"revisado": true}}

	text, err := service.Update(context.Background(), models.Checksum("abc123"), request)

	if err != nil {
		t.Fatalf("Update() unexpected error: %v", err)
	}
	if text.Name != "nuevo nombre" {
		t.Errorf("text.Name = %q, want %q", text.Name, "nuevo nombre")
	}
	if !persistence.updateCalled {
		t.Fatal("persistence.Update was not called")
	}
	if persistence.updateChecksum != models.Checksum("abc123") {
		t.Errorf("update checksum = %q, want %q", persistence.updateChecksum, "abc123")
	}
	if persistence.updatePayload.Name != request.Name {
		t.Errorf("payload.Name = %q, want %q", persistence.updatePayload.Name, request.Name)
	}
	if persistence.updatePayload.Metadata["revisado"] != true {
		t.Errorf("payload.Metadata = %v, want revisado=true", persistence.updatePayload.Metadata)
	}
	if text.Text != "un texto" {
		t.Errorf("text.Text = %q, want %q", text.Text, "un texto")
	}
}

func TestTextServiceUpdateEmitsTextUpdateAuditEventAfterSuccess(t *testing.T) {
	t.Parallel()

	persistence := &stubPersistence{
		updatedText: models.Text{Checksum: models.Checksum("abc123"), Name: "nuevo nombre"},
	}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	text, err := service.Update(context.Background(), models.Checksum("abc123"), dto.UpdateTextRequest{Name: "nuevo nombre"})

	if err != nil {
		t.Fatalf("Update() unexpected error: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want %d", len(audit.events), 1)
	}
	event := audit.events[0]
	if event.Action != models.OpTextUpdate {
		t.Errorf("Action = %q, want %q", event.Action, models.OpTextUpdate)
	}
	if event.EntityType != "text" {
		t.Errorf("EntityType = %q, want %q", event.EntityType, "text")
	}
	if event.Checksum != text.Checksum {
		t.Errorf("Checksum = %q, want %q", event.Checksum, text.Checksum)
	}
	if event.PerformedAt.IsZero() {
		t.Error("PerformedAt is zero")
	}
	details, ok := event.Details.(map[string]any)
	if !ok {
		t.Fatalf("Details = %T, want map[string]any", event.Details)
	}
	if details["name"] != "nuevo nombre" {
		t.Errorf("Details[name] = %v, want %v", details["name"], "nuevo nombre")
	}
}

func TestTextServiceUpdatePropagatesPersistenceError(t *testing.T) {
	t.Parallel()

	updateErr := errors.New("persistence unreachable")
	persistence := &stubPersistence{updateError: updateErr}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	_, err := service.Update(context.Background(), models.Checksum("missing"), dto.UpdateTextRequest{Name: "x"})

	if !errors.Is(err, updateErr) {
		t.Fatalf("error = %v, want %v", err, updateErr)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0 (fallo de update no debe auditarse)", len(audit.events))
	}
}

func TestTextServiceDeleteDelegatesToPersistence(t *testing.T) {
	t.Parallel()

	persistence := &stubPersistence{}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	response, err := service.Delete(context.Background(), models.Checksum("abc123"))

	if err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}
	if response.Message != "OK" {
		t.Errorf("Message = %q, want %q", response.Message, "OK")
	}
	if response.Checksum != models.Checksum("abc123") {
		t.Errorf("Checksum = %q, want %q", response.Checksum, "abc123")
	}
	if !persistence.deleteCalled {
		t.Fatal("persistence.Delete was not called")
	}
	if persistence.deleteChecksum != models.Checksum("abc123") {
		t.Errorf("delete checksum = %q, want %q", persistence.deleteChecksum, "abc123")
	}
}

func TestTextServiceDeleteEmitsTextDeleteAuditEventAfterSuccess(t *testing.T) {
	t.Parallel()

	persistence := &stubPersistence{}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	response, err := service.Delete(context.Background(), models.Checksum("abc123"))

	if err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want %d", len(audit.events), 1)
	}
	event := audit.events[0]
	if event.Action != models.OpTextDelete {
		t.Errorf("Action = %q, want %q", event.Action, models.OpTextDelete)
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
}

func TestTextServiceDeletePropagatesPersistenceError(t *testing.T) {
	t.Parallel()

	deleteErr := errors.New("persistence unreachable")
	persistence := &stubPersistence{deleteError: deleteErr}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	_, err := service.Delete(context.Background(), models.Checksum("abc123"))

	if !errors.Is(err, deleteErr) {
		t.Fatalf("error = %v, want %v", err, deleteErr)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0 (fallo de delete no debe auditarse)", len(audit.events))
	}
}

func TestTextServiceFindByChecksumDelegatesToPersistence(t *testing.T) {
	t.Parallel()

	persistence := &stubPersistence{
		foundText: models.Text{Checksum: models.Checksum("abc123"), Text: "un texto", Name: "mi documento"},
	}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	text, err := service.FindByChecksum(context.Background(), models.Checksum("abc123"))

	if err != nil {
		t.Fatalf("FindByChecksum() unexpected error: %v", err)
	}
	if text.Checksum != models.Checksum("abc123") {
		t.Errorf("text.Checksum = %q, want %q", text.Checksum, "abc123")
	}
	if text.Text != "un texto" {
		t.Errorf("text.Text = %q, want %q", text.Text, "un texto")
	}
	if !persistence.findCalled {
		t.Fatal("persistence.FindByChecksum was not called")
	}
	if persistence.findChecksum != models.Checksum("abc123") {
		t.Errorf("find checksum = %q, want %q", persistence.findChecksum, "abc123")
	}
}

func TestTextServiceFindByChecksumDoesNotEmitAuditEvent(t *testing.T) {
	t.Parallel()

	persistence := &stubPersistence{
		foundText: models.Text{Checksum: models.Checksum("abc123"), Text: "un texto", Name: "mi documento"},
	}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	_, err := service.FindByChecksum(context.Background(), models.Checksum("abc123"))

	if err != nil {
		t.Fatalf("FindByChecksum() unexpected error: %v", err)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0 (leer texto no es accion de auditoria)", len(audit.events))
	}
}

func TestTextServiceFindByChecksumPropagatesNotFound(t *testing.T) {
	t.Parallel()

	notFound := errors.New("persistence: checksum not found")
	persistence := &stubPersistence{findError: notFound}
	audit := &stubAudit{}
	service := NewTextService(persistence, audit)

	_, err := service.FindByChecksum(context.Background(), models.Checksum("missing"))

	if !errors.Is(err, notFound) {
		t.Fatalf("error = %v, want %v", err, notFound)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0 (fallo de find no debe auditarse)", len(audit.events))
	}
}