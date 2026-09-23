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

func testRouter() http.Handler {
	return Routes(minimalConfig(), discardLogger(), handlers.NewPDFHandler(stubPDFService{}, testMaxPDFSize))
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