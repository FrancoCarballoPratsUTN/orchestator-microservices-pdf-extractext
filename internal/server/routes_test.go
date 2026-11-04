package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/handlers"
	"validationmicroservices-pdf-extractext/internal/health"
	"validationmicroservices-pdf-extractext/internal/logging"
	"validationmicroservices-pdf-extractext/internal/models"
)

const testMaxPDFSize = int64(15 * 1024 * 1024)

func discardLogger() *slog.Logger {
	return logging.Discard()
}

func minimalConfig() config.Config {
	return config.Config{Port: "8080"}
}

type stubPDFService struct{}

func (stubPDFService) IngestAndExtract(_ context.Context, _ []byte, _ string) (dto.ExtractPDFResponse, error) {
	return dto.ExtractPDFResponse{Checksum: models.Checksum("abc123"), PageCount: 1, Text: "texto extraido"}, nil
}

type stubAuditService struct{}

func (stubAuditService) LogAsync(_ context.Context, _ models.AuditEvent) {}

func (stubAuditService) Close(_ context.Context) error { return nil }

func (stubAuditService) FetchLogs(_ context.Context, _ dto.AuditQueryParams) (dto.AuditLogsResponse, error) {
	return dto.AuditLogsResponse{Logs: []models.AuditLog{
		{ID: "log-1", Action: "pdf.extract", EntityType: "document"},
	}}, nil
}

type stubTextService struct{}

func (stubTextService) Create(_ context.Context, _ dto.CreateTextRequest) (dto.CreateTextResponse, error) {
	return dto.CreateTextResponse{Message: "OK", Checksum: models.Checksum("abc123")}, nil
}

func (stubTextService) Update(_ context.Context, checksum models.Checksum, _ dto.UpdateTextRequest) (*models.Text, error) {
	return &models.Text{Checksum: checksum, Text: "un texto", Name: "nuevo nombre"}, nil
}

func (stubTextService) FindByChecksum(_ context.Context, checksum models.Checksum) (*models.Text, error) {
	return &models.Text{Checksum: checksum, Text: "un texto", Name: "mi documento"}, nil
}

func (stubTextService) Delete(_ context.Context, checksum models.Checksum) (dto.DeleteTextResponse, error) {
	return dto.DeleteTextResponse{Message: "OK", Checksum: checksum}, nil
}

type stubHealthChecker struct {
	report health.Report
}

func (s stubHealthChecker) Check(context.Context) health.Report { return s.report }

func testRouter() http.Handler {
	return testRouterWithHealth(health.Report{
		Status:       health.StatusOK,
		Dependencies: map[string]health.Dependency{"extract": {Status: health.DependencyOK}},
	})
}

// testRouterWithHealth arma el router real con un reporte de salud fijo.
func testRouterWithHealth(report health.Report) http.Handler {
	return testRouterWithHealthAndLogger(report, discardLogger())
}

func testRouterWithHealthAndLogger(report health.Report, logger *slog.Logger) http.Handler {
	return Routes(
		minimalConfig(),
		logger,
		handlers.NewPDFHandler(handlers.PDFHandlerOptions{
			Service: stubPDFService{},
			MaxSize: testMaxPDFSize,
			Logger:  discardLogger(),
		}),
		handlers.NewAuditHandler(handlers.AuditHandlerOptions{Service: stubAuditService{}, Logger: discardLogger()}),
		handlers.NewTextHandler(handlers.TextHandlerOptions{Service: stubTextService{}, MaxTextBodyBytes: 4 * 1024 * 1024, Logger: discardLogger()}),
		handlers.NewHealthHandler(stubHealthChecker{report: report}, discardLogger()),
	)
}

func TestRequestIDIsReturnedAndLogged(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})
	report := health.Report{Status: health.StatusOK}
	router := testRouterWithHealthAndLogger(report, logger)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit/logs", nil)
	request.Header.Set("X-Request-ID", "req-route")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if got := response.Header().Get("X-Request-ID"); got != "req-route" {
		t.Errorf("X-Request-ID = %q, want %q", got, "req-route")
	}
	record := decodeLogRecord(t, output.Bytes())
	if got := record["request_id"]; got != "req-route" {
		t.Errorf("access log request_id = %v, want %q", got, "req-route")
	}
}

func TestHealthzReturnsOk(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body[status] = %q, want %q", body["status"], "ok")
	}
}

func TestUnknownRouteReturnsNotFound(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestReadyzReturnsOkWhenDependenciesAreHealthy(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body health.Report
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.Status != health.StatusOK {
		t.Errorf("status = %q, want %q", body.Status, health.StatusOK)
	}
	if body.Dependencies["extract"].Status != health.DependencyOK {
		t.Errorf("extract status = %q, want %q", body.Dependencies["extract"].Status, health.DependencyOK)
	}
}

func TestReadyzReturns503WhenADependencyIsDown(t *testing.T) {
	t.Parallel()

	router := testRouterWithHealth(health.Report{
		Status: health.StatusDegraded,
		Dependencies: map[string]health.Dependency{
			"extract": {Status: health.DependencyDown, Detail: "context deadline exceeded"},
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	var body health.Report
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.Status != health.StatusDegraded {
		t.Errorf("status = %q, want %q", body.Status, health.StatusDegraded)
	}
}

func TestCORSHeaderIsPresentOnResponses(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
}

func TestCORSPreflightReturns204(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/texts", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Content-Type")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
	if got := response.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPost) {
		t.Errorf("Access-Control-Allow-Methods = %q, want include %q", got, http.MethodPost)
	}
}

func TestTextCreateRejectsWrongContentTypeAtRouter(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/texts", strings.NewReader(`{"text":"x","checksum":"abc123"}`))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/problem+json")
	}
}

func TestPDFExtractRejectsMissingContentTypeAtRouter(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/pdfs/extract", strings.NewReader("%PDF-1.7"))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
}

func TestExtractRouteIsMounted(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/pdfs/extract", strings.NewReader("%PDF-1.7"))
	request.Header.Set("Content-Type", "application/pdf")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body dto.ExtractPDFResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.Checksum != models.Checksum("abc123") {
		t.Errorf("Checksum = %q, want %q", body.Checksum, "abc123")
	}
	if body.Text != "texto extraido" {
		t.Errorf("Text = %q, want %q", body.Text, "texto extraido")
	}
}

func TestAuditLogsRouteIsMounted(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit/logs", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body dto.AuditLogsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if len(body.Logs) != 1 {
		t.Fatalf("logs count = %d, want %d", len(body.Logs), 1)
	}
	if body.Logs[0].ID != "log-1" {
		t.Errorf("Logs[0].ID = %q, want %q", body.Logs[0].ID, "log-1")
	}
}

func TestTextRouteIsMounted(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/texts", strings.NewReader(`{"text":"un texto","checksum":"abc123"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}
	var body dto.CreateTextResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.Checksum != models.Checksum("abc123") {
		t.Errorf("Checksum = %q, want %q", body.Checksum, "abc123")
	}
}

func TestTextUpdateRouteIsMounted(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/texts/abc123", strings.NewReader(`{"name":"nuevo nombre"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body models.Text
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.Name != "nuevo nombre" {
		t.Errorf("Name = %q, want %q", body.Name, "nuevo nombre")
	}
}

func TestTextDeleteRouteIsMounted(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/texts/abc123", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body dto.DeleteTextResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.Checksum != models.Checksum("abc123") {
		t.Errorf("Checksum = %q, want %q", body.Checksum, "abc123")
	}
}

func TestTextFindRouteIsMounted(t *testing.T) {
	t.Parallel()

	router := testRouter()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/texts/abc123", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body models.Text
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.Text != "un texto" {
		t.Errorf("Text = %q, want %q", body.Text, "un texto")
	}
}
