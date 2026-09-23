package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/services"
)

type stubPDFService struct {
	response dto.ExtractPDFResponse
	err      error
}

func (s *stubPDFService) IngestAndExtract(_ context.Context, _ []byte) (dto.ExtractPDFResponse, error) {
	return s.response, s.err
}

const testMaxPDFSize = int64(15 * 1024 * 1024)

func extractRequest(contentType, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/pdfs/extract", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return request
}

func assertProblem(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d", response.Code, status)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/problem+json")
	}
	var problem httpclient.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("body is not a valid Problem JSON: %v", err)
	}
	if problem.Status != status {
		t.Errorf("Problem.Status = %d, want %d", problem.Status, status)
	}
	if problem.Title == "" {
		t.Error("Problem.Title is empty")
	}
}

func TestPDFHandlerExtractReturns200WithChecksumTextAndPageCount(t *testing.T) {
	t.Parallel()

	handler := NewPDFHandler(&stubPDFService{
		response: dto.ExtractPDFResponse{Checksum: models.Checksum("abc123"), PageCount: 3, Text: "texto extraido"},
	}, testMaxPDFSize)
	response := httptest.NewRecorder()

	handler.Extract(response, extractRequest("application/pdf", "%PDF-1.7"))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	var got dto.ExtractPDFResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	want := dto.ExtractPDFResponse{Checksum: models.Checksum("abc123"), PageCount: 3, Text: "texto extraido"}
	if got != want {
		t.Errorf("body = %+v, want %+v", got, want)
	}
}

func TestPDFHandlerExtractRejectsMissingContentType(t *testing.T) {
	t.Parallel()

	handler := NewPDFHandler(&stubPDFService{}, testMaxPDFSize)
	response := httptest.NewRecorder()

	handler.Extract(response, extractRequest("", "%PDF-1.7"))

	assertProblem(t, response, http.StatusUnsupportedMediaType)
}

func TestPDFHandlerExtractRejectsWrongContentType(t *testing.T) {
	t.Parallel()

	handler := NewPDFHandler(&stubPDFService{}, testMaxPDFSize)
	response := httptest.NewRecorder()

	handler.Extract(response, extractRequest("text/plain", "%PDF-1.7"))

	assertProblem(t, response, http.StatusUnsupportedMediaType)
}

func TestPDFHandlerExtractRejectsPayloadOverSizeLimit(t *testing.T) {
	t.Parallel()

	handler := NewPDFHandler(&stubPDFService{}, 10)
	response := httptest.NewRecorder()

	handler.Extract(response, extractRequest("application/pdf", strings.Repeat("A", 100)))

	assertProblem(t, response, http.StatusRequestEntityTooLarge)
}

func TestPDFHandlerExtractReturns400WhenServiceRejectsInvalidPDF(t *testing.T) {
	t.Parallel()

	handler := NewPDFHandler(&stubPDFService{err: services.ErrInvalidPDF}, testMaxPDFSize)
	response := httptest.NewRecorder()

	handler.Extract(response, extractRequest("application/pdf", "no soy un pdf"))

	assertProblem(t, response, http.StatusBadRequest)
}

func TestPDFHandlerExtractReturns502WhenExtractClientFails(t *testing.T) {
	t.Parallel()

	handler := NewPDFHandler(&stubPDFService{err: errors.New("extract unreachable")}, testMaxPDFSize)
	response := httptest.NewRecorder()

	handler.Extract(response, extractRequest("application/pdf", "%PDF-1.7"))

	assertProblem(t, response, http.StatusBadGateway)
}