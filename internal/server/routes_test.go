package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/handlers"
	"validationmicroservices-pdf-extractext/internal/models"
)

const testMaxPDFSize = int64(15 * 1024 * 1024)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func minimalConfig() config.Config {
	return config.Config{Port: "8080"}
}

type stubPDFService struct{}

func (stubPDFService) IngestAndExtract(_ context.Context, _ []byte) (dto.ExtractPDFResponse, error) {
	return dto.ExtractPDFResponse{Checksum: models.Checksum("abc123"), PageCount: 1, Text: "texto extraido"}, nil
}

type stubAuditService struct{}

func (stubAuditService) LogAsync(_ context.Context, _ models.AuditEvent) {}

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

func (stubTextService) Delete(_ context.Context, checksum models.Checksum) (dto.DeleteTextResponse, error) {
	return dto.DeleteTextResponse{Message: "OK", Checksum: checksum}, nil
}

func testRouter() http.Handler {
	return Routes(
		minimalConfig(),
		discardLogger(),
		handlers.NewPDFHandler(stubPDFService{}, testMaxPDFSize),
		handlers.NewAuditHandler(stubAuditService{}),
		handlers.NewTextHandler(stubTextService{}),
	)
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