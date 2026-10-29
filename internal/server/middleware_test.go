package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/logging"
	"validationmicroservices-pdf-extractext/internal/reqctx"
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

func TestWithRecoveryLogsRequestIDOnPanic(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})
	handler := withRequestID(withRecovery(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/texts/abc123", nil)
	request.Header.Set("X-Request-ID", "req-panic")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	record := decodeLogRecord(t, output.Bytes())
	if got := record["request_id"]; got != "req-panic" {
		t.Errorf("request_id = %v, want %q", got, "req-panic")
	}
}

func TestWithRequestIDEchoesIncomingHeaderAndAddsContext(t *testing.T) {
	t.Parallel()

	var gotRequestID string
	handler := withRequestID(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		gotRequestID = reqctx.RequestID(request.Context())
	}))
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "req-123")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if got := response.Header().Get("X-Request-ID"); got != "req-123" {
		t.Errorf("X-Request-ID = %q, want %q", got, "req-123")
	}
	if gotRequestID != "req-123" {
		t.Errorf("request ID in context = %q, want %q", gotRequestID, "req-123")
	}
}

func TestWithRequestIDGeneratesIDWhenMissing(t *testing.T) {
	t.Parallel()

	var requestIDFromContext string
	handler := withRequestID(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		requestIDFromContext = reqctx.RequestID(request.Context())
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	requestIDFromHeader := response.Header().Get("X-Request-ID")
	if requestIDFromHeader == "" {
		t.Fatal("X-Request-ID header is empty")
	}
	if requestIDFromContext != requestIDFromHeader {
		t.Errorf("context request ID = %q, header = %q", requestIDFromContext, requestIDFromHeader)
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
		t.Errorf("generated request IDs are equal: %q", first.Header().Get("X-Request-ID"))
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
