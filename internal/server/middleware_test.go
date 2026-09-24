package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"validationmicroservices-pdf-extractext/internal/httpclient"
)

func TestWithRecoveryReturns500ProblemOnPanic(t *testing.T) {
	t.Parallel()

	handler := withRecovery(discardLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/texts/abc123", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/problem+json")
	}
	var problem httpclient.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("body is not a valid Problem JSON: %v", err)
	}
	if problem.Title != "Internal Server Error" {
		t.Errorf("Title = %q, want %q", problem.Title, "Internal Server Error")
	}
}

func TestWithRecoveryDoesNotInterfereWithNormalHandler(t *testing.T) {
	t.Parallel()

	handler := withRecovery(discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestWithRequestIDEchoesIncomingHeader(t *testing.T) {
	t.Parallel()

	var gotRequestID string
	handler := withRequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotRequestID = requestIDFromContext(r.Context())
	}))
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "req-123")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if got := response.Header().Get("X-Request-ID"); got != "req-123" {
		t.Errorf("X-Request-ID = %q, want %q", got, "req-123")
	}
	if gotRequestID != "req-123" {
		t.Errorf("request id en contexto = %q, want %q", gotRequestID, "req-123")
	}
}

func TestWithRequestIDGeneratesIDWhenMissing(t *testing.T) {
	t.Parallel()

	var gotRequestID string
	handler := withRequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotRequestID = requestIDFromContext(r.Context())
	}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	header := response.Header().Get("X-Request-ID")
	if header == "" {
		t.Error("X-Request-ID header vacío, se esperaba un valor generado")
	}
	if gotRequestID == "" {
		t.Error("request id en contexto vacío, se esperaba un valor generado")
	}
	if gotRequestID != header {
		t.Errorf("request id en contexto %q != header %q", gotRequestID, header)
	}
}

func TestWithRequestIDGeneratesUniqueIDs(t *testing.T) {
	t.Parallel()

	handler := withRequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	first := httptest.NewRecorder()
	second := httptest.NewRecorder()

	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if first.Header().Get("X-Request-ID") == second.Header().Get("X-Request-ID") {
		t.Errorf("X-Request-ID repetido entre peticiones: %q", first.Header().Get("X-Request-ID"))
	}
}

func contentTypeRequest(method, path, contentType string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return request
}

func TestEnforceContentTypeAllowsMatchingType(t *testing.T) {
	t.Parallel()

	called := false
	handler := enforceContentType("application/json")(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, contentTypeRequest(http.MethodPost, "/api/v1/texts", "application/json"))

	if !called {
		t.Error("handler de abajo no fue invocado con content-type esperado")
	}
	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestEnforceContentTypeAcceptsMediaTypeWithParameters(t *testing.T) {
	t.Parallel()

	called := false
	handler := enforceContentType("application/json")(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, contentTypeRequest(http.MethodPost, "/api/v1/texts", "application/json; charset=utf-8"))

	if !called {
		t.Error("handler de abajo no fue invocado con media type que incluye parámetros")
	}
}

func TestEnforceContentTypeRejectsMissingType(t *testing.T) {
	t.Parallel()

	handler := enforceContentType("application/json")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, contentTypeRequest(http.MethodPost, "/api/v1/texts", ""))

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
	var problem httpclient.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("body is not a valid Problem JSON: %v", err)
	}
	if problem.Detail != "expected Content-Type application/json" {
		t.Errorf("Problem.Detail = %q, want %q", problem.Detail, "expected Content-Type application/json")
	}
}

func TestEnforceContentTypeRejectsWrongType(t *testing.T) {
	t.Parallel()

	handler := enforceContentType("application/json")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, contentTypeRequest(http.MethodPost, "/api/v1/texts", "text/plain"))

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
}