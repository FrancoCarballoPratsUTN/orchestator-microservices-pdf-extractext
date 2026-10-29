package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"validationmicroservices-pdf-extractext/internal/health"
)

type stubChecker struct {
	report health.Report
}

func (s stubChecker) Check(context.Context) health.Report { return s.report }

func TestReadyReturnsOKWhenAllDependenciesAreHealthy(t *testing.T) {
	t.Parallel()

	handler := NewHealthHandler(stubChecker{report: health.Report{
		Status: health.StatusOK,
		Dependencies: map[string]health.Dependency{
			"extract":     {Status: health.DependencyOK, DurationMS: 3},
			"persistence": {Status: health.DependencyOK, DurationMS: 5},
		},
	}}, discardLogger())

	response := httptest.NewRecorder()
	handler.Ready(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body health.Report
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.Status != health.StatusOK {
		t.Errorf("Status = %q, want %q", body.Status, health.StatusOK)
	}
	if len(body.Dependencies) != 2 {
		t.Errorf("len(Dependencies) = %d, want 2", len(body.Dependencies))
	}
}

func TestReadyReturns503WhenADependencyIsDown(t *testing.T) {
	t.Parallel()

	handler := NewHealthHandler(stubChecker{report: health.Report{
		Status: health.StatusDegraded,
		Dependencies: map[string]health.Dependency{
			"extract":     {Status: health.DependencyOK, DurationMS: 3},
			"persistence": {Status: health.DependencyDown, DurationMS: 2001, Detail: "context deadline exceeded"},
		},
	}}, discardLogger())

	response := httptest.NewRecorder()
	handler.Ready(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	var body health.Report
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.Status != health.StatusDegraded {
		t.Errorf("Status = %q, want %q", body.Status, health.StatusDegraded)
	}
	down := body.Dependencies["persistence"]
	if down.Status != health.DependencyDown {
		t.Errorf("persistence.Status = %q, want %q", down.Status, health.DependencyDown)
	}
	if down.Detail == "" {
		t.Error("persistence.Detail is empty, want the failure reason")
	}
}

func TestReadyRequiresNoAuthenticationAndIsNotUnderAPI(t *testing.T) {
	t.Parallel()

	handler := NewHealthHandler(stubChecker{report: health.Report{Status: health.StatusOK}}, discardLogger())

	response := httptest.NewRecorder()
	handler.Ready(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d without any credentials", response.Code, http.StatusOK)
	}
}
