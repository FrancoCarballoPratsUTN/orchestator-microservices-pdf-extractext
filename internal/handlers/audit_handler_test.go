package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/services"
)

type stubAuditService struct {
	response dto.AuditLogsResponse
	err      error
	params   dto.AuditQueryParams
	calls    int
}

func (s *stubAuditService) LogAsync(_ context.Context, _ models.AuditEvent) {}

func (s *stubAuditService) FetchLogs(_ context.Context, params dto.AuditQueryParams) (dto.AuditLogsResponse, error) {
	s.calls++
	s.params = params
	return s.response, s.err
}

var _ services.AuditService = (*stubAuditService)(nil)

func auditLogsRequest(rawQuery string) *http.Request {
	path := "/api/v1/audit/logs"
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	return httptest.NewRequest(http.MethodGet, path, nil)
}

func assertLogs(t *testing.T, response *httptest.ResponseRecorder, want []models.AuditLog) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	var body dto.AuditLogsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not a valid AuditLogsResponse JSON: %v", err)
	}
	if len(body.Logs) != len(want) {
		t.Fatalf("logs count = %d, want %d (body: %s)", len(body.Logs), len(want), response.Body.String())
	}
	for i := range want {
		if body.Logs[i].ID != want[i].ID {
			t.Errorf("Logs[%d].ID = %q, want %q", i, body.Logs[i].ID, want[i].ID)
		}
	}
}

func TestAuditHandlerListPassesQueryParamsToFetchLogs(t *testing.T) {
	t.Parallel()

	service := &stubAuditService{}
	handler := NewAuditHandler(service)

	response := httptest.NewRecorder()
	handler.List(response, auditLogsRequest("checksum=abc123&skip=5&limit=20"))

	if service.calls != 1 {
		t.Fatalf("FetchLogs calls = %d, want %d", service.calls, 1)
	}
	want := dto.AuditQueryParams{Checksum: models.Checksum("abc123"), Skip: 5, Limit: 20}
	if service.params != want {
		t.Errorf("params = %+v, want %+v", service.params, want)
	}
	assertLogs(t, response, nil)
}

func TestAuditHandlerListDefaultsSkipAndLimit(t *testing.T) {
	t.Parallel()

	service := &stubAuditService{}
	handler := NewAuditHandler(service)

	response := httptest.NewRecorder()
	handler.List(response, auditLogsRequest(""))

	want := dto.AuditQueryParams{Checksum: "", Skip: 0, Limit: 10}
	if service.params != want {
		t.Errorf("params = %+v, want %+v", service.params, want)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestAuditHandlerListReturnsWrappedLogs(t *testing.T) {
	t.Parallel()

	service := &stubAuditService{
		response: dto.AuditLogsResponse{Logs: []models.AuditLog{
			{ID: "log-1", Action: "pdf.extract", EntityType: "document", Checksum: "abc123"},
			{ID: "log-2", Action: "pdf.extract", EntityType: "document"},
		}},
	}
	handler := NewAuditHandler(service)

	response := httptest.NewRecorder()
	handler.List(response, auditLogsRequest("checksum=abc123"))

	assertLogs(t, response, []models.AuditLog{
		{ID: "log-1"},
		{ID: "log-2"},
	})
}

func TestAuditHandlerListRejectsInvalidSkip(t *testing.T) {
	t.Parallel()

	handler := NewAuditHandler(&stubAuditService{})

	response := httptest.NewRecorder()
	handler.List(response, auditLogsRequest("skip=abc"))

	assertProblem(t, response, http.StatusBadRequest)
}

func TestAuditHandlerListRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	handler := NewAuditHandler(&stubAuditService{})

	response := httptest.NewRecorder()
	handler.List(response, auditLogsRequest("limit=-3x"))

	assertProblem(t, response, http.StatusBadRequest)
}

func TestAuditHandlerListPropagatesProblemFromAuditLogService(t *testing.T) {
	t.Parallel()

	service := &stubAuditService{
		err: httpclient.Problem{Type: "about:blank", Title: "Not Found", Status: http.StatusNotFound, Detail: "no logs for checksum"},
	}
	handler := NewAuditHandler(service)

	response := httptest.NewRecorder()
	handler.List(response, auditLogsRequest("checksum=missing"))

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
	if problem.Status != http.StatusNotFound {
		t.Errorf("Problem.Status = %d, want %d", problem.Status, http.StatusNotFound)
	}
	if problem.Detail != "no logs for checksum" {
		t.Errorf("Problem.Detail = %q, want %q", problem.Detail, "no logs for checksum")
	}
}

func TestAuditHandlerListReturns502OnTransportError(t *testing.T) {
	t.Parallel()

	service := &stubAuditService{err: errors.New("audit log unreachable")}
	handler := NewAuditHandler(service)

	response := httptest.NewRecorder()
	handler.List(response, auditLogsRequest(""))

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadGateway)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/problem+json")
	}
}