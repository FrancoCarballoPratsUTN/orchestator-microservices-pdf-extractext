package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/logging"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/reqctx"
)

const testEmitTimeout = time.Second

func discardLogger() *slog.Logger {
	return logging.Discard()
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

// gatedEmitClient bloquea cada Emit hasta que el test cierra release, y avisa por
// started que la llamada ya entró. Permite comprobar que Close espera a las
// emisiones en vuelo y que respeta el deadline del contexto.
type gatedEmitClient struct {
	calls   int32
	started chan struct{}
	release chan struct{}
}

func (g *gatedEmitClient) Emit(_ context.Context, _ models.AuditEvent) error {
	atomic.AddInt32(&g.calls, 1)
	g.started <- struct{}{}
	<-g.release
	return nil
}

func (g *gatedEmitClient) ListAll(_ context.Context, _, _ int) ([]models.AuditLog, error) {
	return nil, nil
}

func (g *gatedEmitClient) ListByChecksum(_ context.Context, _ models.Checksum) ([]models.AuditLog, error) {
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

func TestAuditServiceCloseWaitsForPendingEmits(t *testing.T) {
	t.Parallel()

	const emits = 3
	client := &gatedEmitClient{
		started: make(chan struct{}, emits),
		release: make(chan struct{}),
	}
	service := NewAuditService(client, discardLogger(), testEmitTimeout)

	for i := 0; i < emits; i++ {
		service.LogAsync(context.Background(), testAuditEvent())
	}
	for i := 0; i < emits; i++ {
		select {
		case <-client.started:
		case <-time.After(time.Second):
			t.Fatalf("only %d of %d emits started", i, emits)
		}
	}

	closed := make(chan error, 1)
	go func() { closed <- service.Close(context.Background()) }()

	// Close no debe volver mientras las emisiones sigan en vuelo: si vuelve acá,
	// el apagado estaría descartando eventos ya agendados.
	select {
	case err := <-closed:
		t.Fatalf("Close returned while %d emits were still in-flight (err=%v)", emits, err)
	case <-time.After(100 * time.Millisecond):
	}

	close(client.release)

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not return after pending emits finished")
	}
	if got := atomic.LoadInt32(&client.calls); got != emits {
		t.Errorf("Emit calls = %d, want %d", got, emits)
	}
}

func TestAuditServiceCloseRespectsContextDeadline(t *testing.T) {
	t.Parallel()

	client := &gatedEmitClient{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	service := NewAuditService(client, discardLogger(), testEmitTimeout)
	service.LogAsync(context.Background(), testAuditEvent())
	<-client.started

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := service.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() error = %v, want %v", err, context.DeadlineExceeded)
	}
	close(client.release)
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

func TestAuditServiceEmitFailureLogIncludesRequestID(t *testing.T) {
	t.Parallel()

	client := &stubAuditClient{emitErr: errors.New("audit ms down")}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	service := &auditService{client: client, logger: logger, timeout: testEmitTimeout}

	ctx := reqctx.WithRequestID(context.Background(), "req-audit")
	service.emit(testAuditEvent(), ctx)

	if !strings.Contains(output.String(), `"request_id":"req-audit"`) {
		t.Errorf("audit failure log does not carry request_id: %q", output.String())
	}
}

func TestAuditServiceEmitDoesNotRetryUnauthorizedAndLogsError(t *testing.T) {
	t.Parallel()

	client := &stubAuditClient{emitErr: httpclient.Problem{
		Title:  "Unauthorized",
		Status: http.StatusUnauthorized,
		Detail: "missing bearer token",
	}}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	service := &auditService{client: client, logger: logger, timeout: testEmitTimeout}

	service.emit(testAuditEvent(), context.Background())

	if client.emitCalls != 1 {
		t.Errorf("Emit calls = %d, want %d (a 401 is a wiring error, not retried)", client.emitCalls, 1)
	}
	if !strings.Contains(output.String(), `"level":"ERROR"`) {
		t.Errorf("log output missing ERROR record for a 401: %q", output.String())
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
