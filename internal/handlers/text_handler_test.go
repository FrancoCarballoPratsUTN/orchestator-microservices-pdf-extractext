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
	response       dto.CreateTextResponse
	err            error
	updateText     models.Text
	updateErr      error
	updateCalls    int
	updateChecksum models.Checksum
	updateRequest  dto.UpdateTextRequest
	deleteResponse dto.DeleteTextResponse
	deleteErr      error
	deleteCalls    int
	deleteChecksum models.Checksum
}

func (s *stubTextService) Create(_ context.Context, _ dto.CreateTextRequest) (dto.CreateTextResponse, error) {
	return s.response, s.err
}

func (s *stubTextService) Update(_ context.Context, checksum models.Checksum, req dto.UpdateTextRequest) (*models.Text, error) {
	s.updateCalls++
	s.updateChecksum = checksum
	s.updateRequest = req
	return &s.updateText, s.updateErr
}

func (s *stubTextService) Delete(_ context.Context, checksum models.Checksum) (dto.DeleteTextResponse, error) {
	s.deleteCalls++
	s.deleteChecksum = checksum
	return s.deleteResponse, s.deleteErr
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

func updateRequest(path, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func deleteRequest(path string) *http.Request {
	return httptest.NewRequest(http.MethodDelete, path, nil)
}

func TestTextHandlerUpdateReturns200WithUpdatedText(t *testing.T) {
	t.Parallel()

	service := &stubTextService{
		updateText: models.Text{Checksum: models.Checksum("abc123"), Text: "un texto", Name: "nuevo nombre"},
	}
	handler := NewTextHandler(service)
	response := httptest.NewRecorder()

	handler.Update(response, updateRequest("/api/v1/texts/abc123", `{"name":"nuevo nombre"}`))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	var body models.Text
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not a valid JSON: %v", err)
	}
	if body.Name != "nuevo nombre" {
		t.Errorf("Name = %q, want %q", body.Name, "nuevo nombre")
	}
	if service.updateCalls != 1 {
		t.Fatalf("service.Update calls = %d, want %d", service.updateCalls, 1)
	}
	if service.updateChecksum != models.Checksum("abc123") {
		t.Errorf("checksum = %q, want %q", service.updateChecksum, "abc123")
	}
	if service.updateRequest.Name != "nuevo nombre" {
		t.Errorf("request.Name = %q, want %q", service.updateRequest.Name, "nuevo nombre")
	}
}

func TestTextHandlerUpdateRejectsTextInBody(t *testing.T) {
	t.Parallel()

	service := &stubTextService{}
	handler := NewTextHandler(service)
	response := httptest.NewRecorder()

	handler.Update(response, updateRequest("/api/v1/texts/abc123", `{"name":"x","text":"tocado"}`))

	assertProblem(t, response, http.StatusBadRequest)
	if service.updateCalls != 0 {
		t.Errorf("service.Update calls = %d, want 0 (regla de inmutabilidad, sin tocar el servicio)", service.updateCalls)
	}
}

func TestTextHandlerUpdateRejectsChecksumInBody(t *testing.T) {
	t.Parallel()

	service := &stubTextService{}
	handler := NewTextHandler(service)
	response := httptest.NewRecorder()

	handler.Update(response, updateRequest("/api/v1/texts/abc123", `{"name":"x","checksum":"otro"}`))

	assertProblem(t, response, http.StatusBadRequest)
	if service.updateCalls != 0 {
		t.Errorf("service.Update calls = %d, want 0 (regla de inmutabilidad, sin tocar el servicio)", service.updateCalls)
	}
}

func TestTextHandlerUpdateRejectsNonJSONContentType(t *testing.T) {
	t.Parallel()

	handler := NewTextHandler(&stubTextService{})
	request := httptest.NewRequest(http.MethodPut, "/api/v1/texts/abc123", strings.NewReader(`{"name":"x"}`))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	handler.Update(response, request)

	assertProblem(t, response, http.StatusUnsupportedMediaType)
}

func TestTextHandlerUpdateRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	handler := NewTextHandler(&stubTextService{})
	response := httptest.NewRecorder()

	handler.Update(response, updateRequest("/api/v1/texts/abc123", `{"name":`))

	assertProblem(t, response, http.StatusBadRequest)
}

func TestTextHandlerUpdatePropagates404ProblemFromPersistence(t *testing.T) {
	t.Parallel()

	service := &stubTextService{
		updateErr: httpclient.Problem{Type: "about:blank", Title: "Not Found", Status: http.StatusNotFound, Detail: "checksum not found"},
	}
	handler := NewTextHandler(service)
	response := httptest.NewRecorder()

	handler.Update(response, updateRequest("/api/v1/texts/missing", `{"name":"x"}`))

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/problem+json")
	}
	var problem httpclient.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("body is not a valid Problem JSON: %v", err)
	}
	if problem.Detail != "checksum not found" {
		t.Errorf("Problem.Detail = %q, want %q", problem.Detail, "checksum not found")
	}
}

func TestTextHandlerUpdateReturns502OnTransportError(t *testing.T) {
	t.Parallel()

	handler := NewTextHandler(&stubTextService{updateErr: errors.New("persistence unreachable")})
	response := httptest.NewRecorder()

	handler.Update(response, updateRequest("/api/v1/texts/abc123", `{"name":"x"}`))

	assertProblem(t, response, http.StatusBadGateway)
}

func TestTextHandlerDeleteReturns200WithMessageAndChecksum(t *testing.T) {
	t.Parallel()

	service := &stubTextService{deleteResponse: dto.DeleteTextResponse{Message: "OK", Checksum: models.Checksum("abc123")}}
	handler := NewTextHandler(service)
	response := httptest.NewRecorder()

	handler.Delete(response, deleteRequest("/api/v1/texts/abc123"))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body dto.DeleteTextResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not a valid JSON: %v", err)
	}
	if body.Message != "OK" {
		t.Errorf("Message = %q, want %q", body.Message, "OK")
	}
	if body.Checksum != models.Checksum("abc123") {
		t.Errorf("Checksum = %q, want %q", body.Checksum, "abc123")
	}
	if service.deleteCalls != 1 {
		t.Fatalf("service.Delete calls = %d, want %d", service.deleteCalls, 1)
	}
	if service.deleteChecksum != models.Checksum("abc123") {
		t.Errorf("checksum = %q, want %q", service.deleteChecksum, "abc123")
	}
}

func TestTextHandlerDeletePropagates404ProblemFromPersistence(t *testing.T) {
	t.Parallel()

	service := &stubTextService{
		deleteErr: httpclient.Problem{Type: "about:blank", Title: "Not Found", Status: http.StatusNotFound, Detail: "checksum not found"},
	}
	handler := NewTextHandler(service)
	response := httptest.NewRecorder()

	handler.Delete(response, deleteRequest("/api/v1/texts/missing"))

	assertProblem(t, response, http.StatusNotFound)
}

func TestTextHandlerDeleteReturns502OnTransportError(t *testing.T) {
	t.Parallel()

	handler := NewTextHandler(&stubTextService{deleteErr: errors.New("persistence unreachable")})
	response := httptest.NewRecorder()

	handler.Delete(response, deleteRequest("/api/v1/texts/abc123"))

	assertProblem(t, response, http.StatusBadGateway)
}