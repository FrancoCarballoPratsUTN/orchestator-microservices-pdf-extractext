package services

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
)

const testEmitTimeout = time.Second

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func testAuditEvent() models.AuditEvent {
	return models.AuditEvent{
		Action:      models.OpPDFExtract,
		EntityType:  "document",
		Checksum:    models.Checksum("abc123"),
		Details:     map[string]any{"page_count": 3},
		PerformedAt: time.Now(),
	}
}

type stubAuditClient struct {
	emitErr          error
	emitCalls        int
	emitted          []models.AuditEvent
	emitSignal       chan struct{}
	listErr          error
	logs             []models.AuditLog
	receivedChecksum models.Checksum
	receivedSkip     int
	receivedLimit    int
}

func (s *stubAuditClient) Emit(_ context.Context, event models.AuditEvent) error {
	s.emitCalls++
	s.emitted = append(s.emitted, event)
	if s.emitSignal != nil {
		s.emitSignal <- struct{}{}
	}
	return s.emitErr
}

func (s *stubAuditClient) ListAll(_ context.Context, skip, limit int) ([]models.AuditLog, error) {
	s.receivedSkip = skip
	s.receivedLimit = limit
	return s.logs, s.listErr
}

func (s *stubAuditClient) ListByChecksum(_ context.Context, checksum models.Checksum) ([]models.AuditLog, error) {
	s.receivedChecksum = checksum
	return s.logs, s.listErr
}

type blockingEmitClient struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingEmitClient) Emit(_ context.Context, _ models.AuditEvent) error {
	close(b.started)
	<-b.release
	return nil
}

func (b *blockingEmitClient) ListAll(_ context.Context, _, _ int) ([]models.AuditLog, error) {
	return nil, nil
}

func (b *blockingEmitClient) ListByChecksum(_ context.Context, _ models.Checksum) ([]models.AuditLog, error) {
	return nil, nil
}

func TestAuditServiceLogAsyncReturnsWithoutWaitingForEmit(t *testing.T) {
	t.Parallel()

	blocked := &blockingEmitClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := NewAuditService(blocked, discardLogger(), testEmitTimeout)

	returned := make(chan struct{})
	go func() {
		service.LogAsync(context.Background(), testAuditEvent())
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("LogAsync blocked the caller while the emit was still in progress")
	}
	close(blocked.release)
}

func TestAuditServiceLogAsyncEmitsEventWithOriginalValues(t *testing.T) {
	t.Parallel()

	client := &stubAuditClient{emitSignal: make(chan struct{}, 1)}
	service := NewAuditService(client, discardLogger(), testEmitTimeout)

	event := testAuditEvent()
	service.LogAsync(context.Background(), event)

	select {
	case <-client.emitSignal:
	case <-time.After(time.Second):
		t.Fatal("audit emit was not executed in background")
	}
	if client.emitCalls != 1 {
		t.Errorf("Emit calls = %d, want %d", client.emitCalls, 1)
	}
	got := client.emitted[0]
	if got.Action != event.Action {
		t.Errorf("Action = %q, want %q", got.Action, event.Action)
	}
	if got.EntityType != event.EntityType {
		t.Errorf("EntityType = %q, want %q", got.EntityType, event.EntityType)
	}
	if got.Checksum != event.Checksum {
		t.Errorf("Checksum = %q, want %q", got.Checksum, event.Checksum)
	}
	if got.PerformedAt != event.PerformedAt {
		t.Errorf("PerformedAt = %v, want %v", got.PerformedAt, event.PerformedAt)
	}
}

func TestAuditServiceEmitSucceedsOnFirstAttemptAndDoesNotRetry(t *testing.T) {
	t.Parallel()

	client := &stubAuditClient{}
	service := &auditService{client: client, logger: discardLogger(), timeout: testEmitTimeout}

	service.emit(testAuditEvent(), context.Background())

	if client.emitCalls != 1 {
		t.Errorf("Emit calls = %d, want %d", client.emitCalls, 1)
	}
}

func TestAuditServiceEmitRetriesThenLogsFailure(t *testing.T) {
	t.Parallel()

	client := &stubAuditClient{emitErr: errors.New("audit ms down")}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	service := &auditService{client: client, logger: logger, timeout: testEmitTimeout}

	service.emit(testAuditEvent(), context.Background())

	if client.emitCalls != maxEmitAttempts {
		t.Errorf("Emit calls = %d, want %d (intento + reintentos)", client.emitCalls, maxEmitAttempts)
	}
	if !strings.Contains(output.String(), "audit emit failed") {
		t.Errorf("log output missing failure record: %q", output.String())
	}
}

func TestAuditServiceFetchLogsListsAllWhenNoChecksum(t *testing.T) {
	t.Parallel()

	client := &stubAuditClient{
		logs: []models.AuditLog{{ID: "log-1", Action: "pdf.extract"}},
	}
	service := NewAuditService(client, discardLogger(), testEmitTimeout)

	response, err := service.FetchLogs(context.Background(), dto.AuditQueryParams{Skip: 5, Limit: 20})

	if err != nil {
		t.Fatalf("FetchLogs() unexpected error: %v", err)
	}
	if len(response.Logs) != 1 || response.Logs[0].ID != "log-1" {
		t.Errorf("Logs = %+v, want a single log-1", response.Logs)
	}
	if client.receivedSkip != 5 {
		t.Errorf("skip = %d, want %d", client.receivedSkip, 5)
	}
	if client.receivedLimit != 20 {
		t.Errorf("limit = %d, want %d", client.receivedLimit, 20)
	}
}

func TestAuditServiceFetchLogsByChecksumWhenChecksumProvided(t *testing.T) {
	t.Parallel()

	client := &stubAuditClient{logs: []models.AuditLog{{ID: "log-1"}}}
	service := NewAuditService(client, discardLogger(), testEmitTimeout)

	response, err := service.FetchLogs(context.Background(), dto.AuditQueryParams{Checksum: models.Checksum("abc123")})

	if err != nil {
		t.Fatalf("FetchLogs() unexpected error: %v", err)
	}
	if len(response.Logs) != 1 {
		t.Errorf("Logs length = %d, want %d", len(response.Logs), 1)
	}
	if client.receivedChecksum != models.Checksum("abc123") {
		t.Errorf("checksum = %q, want %q", client.receivedChecksum, "abc123")
	}
}

func TestAuditServiceFetchLogsPropagatesClientError(t *testing.T) {
	t.Parallel()

	listErr := errors.New("audit ms unreachable")
	client := &stubAuditClient{listErr: listErr}
	service := NewAuditService(client, discardLogger(), testEmitTimeout)

	_, err := service.FetchLogs(context.Background(), dto.AuditQueryParams{})

	if !errors.Is(err, listErr) {
		t.Fatalf("error = %v, want %v", err, listErr)
	}
}