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

type stubTextService struct {
	response dto.CreateTextResponse
	err      error
}

func (s *stubTextService) Create(_ context.Context, _ dto.CreateTextRequest) (dto.CreateTextResponse, error) {
	return s.response, s.err
}

var _ services.TextService = (*stubTextService)(nil)

func textRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/texts", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestTextHandlerCreateReturns201WithMessageAndChecksum(t *testing.T) {
	t.Parallel()

	handler := NewTextHandler(&stubTextService{
		response: dto.CreateTextResponse{Message: "OK", Checksum: models.Checksum("abc123")},
	})
	response := httptest.NewRecorder()

	handler.Create(response, textRequest(`{"text":"un texto","checksum":"abc123","name":"mi documento"}`))

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	var body dto.CreateTextResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not a valid JSON: %v", err)
	}
	if body.Message != "OK" {
		t.Errorf("Message = %q, want %q", body.Message, "OK")
	}
	if body.Checksum != models.Checksum("abc123") {
		t.Errorf("Checksum = %q, want %q", body.Checksum, "abc123")
	}
}

func TestTextHandlerCreateRejectsNonJSONContentType(t *testing.T) {
	t.Parallel()

	handler := NewTextHandler(&stubTextService{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/texts", strings.NewReader(`{"text":"x"}`))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	handler.Create(response, request)

	assertProblem(t, response, http.StatusUnsupportedMediaType)
}

func TestTextHandlerCreateRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	handler := NewTextHandler(&stubTextService{})
	response := httptest.NewRecorder()

	handler.Create(response, textRequest(`{"text":`))

	assertProblem(t, response, http.StatusBadRequest)
}

func TestTextHandlerCreateMapsEmptyChecksumTo400(t *testing.T) {
	t.Parallel()

	handler := NewTextHandler(&stubTextService{err: services.ErrEmptyChecksum})
	response := httptest.NewRecorder()

	handler.Create(response, textRequest(`{"text":"sin checksum"}`))

	assertProblem(t, response, http.StatusBadRequest)
}

func TestTextHandlerCreatePropagates409ProblemFromPersistence(t *testing.T) {
	t.Parallel()

	handler := NewTextHandler(&stubTextService{
		err: httpclient.Problem{Type: "about:blank", Title: "Conflict", Status: http.StatusConflict, Detail: "checksum already exists"},
	})
	response := httptest.NewRecorder()

	handler.Create(response, textRequest(`{"text":"x","checksum":"dup"}`))

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/problem+json")
	}
	var problem httpclient.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("body is not a valid Problem JSON: %v", err)
	}
	if problem.Detail != "checksum already exists" {
		t.Errorf("Problem.Detail = %q, want %q", problem.Detail, "checksum already exists")
	}
}

func TestTextHandlerCreateReturns502OnTransportError(t *testing.T) {
	t.Parallel()

	handler := NewTextHandler(&stubTextService{err: errors.New("persistence unreachable")})
	response := httptest.NewRecorder()

	handler.Create(response, textRequest(`{"text":"x","checksum":"abc"}`))

	assertProblem(t, response, http.StatusBadGateway)
}