package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/logging"
	"validationmicroservices-pdf-extractext/internal/reqctx"
)

func TestWithRecoveryReturns500ProblemOnPanic(t *testing.T) {
	t.Parallel()

	handler := withRecovery(discardLogger(), logging.Limits{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
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

	handler := withRecovery(discardLogger(), logging.Limits{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestRecoveryDoesNotWriteProblemAfterFlush(t *testing.T) {
	t.Parallel()

	handler := withRequestLog(discardLogger(), logging.Limits{})(withRecovery(discardLogger(), logging.Limits{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush()
		panic("boom after flush")
	})))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/stream", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(response.Body.String(), "Internal Server Error") {
		t.Fatalf("recovery wrote a second response after Flush: %s", response.Body.String())
	}
}

type unwrapOnlyWriter struct {
	http.ResponseWriter
}

func (w unwrapOnlyWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func TestRecoveryDoesNotWriteProblemThroughUnwrapWrapper(t *testing.T) {
	t.Parallel()

	wrap := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(unwrapOnlyWriter{ResponseWriter: w}, r)
		})
	}
	handler := withRequestLog(discardLogger(), logging.Limits{})(
		wrap(withRecovery(discardLogger(), logging.Limits{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			panic("boom after write")
		}))),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/wrapped", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(response.Body.String(), "Internal Server Error") {
		t.Fatalf("recovery wrote a second response through a wrapper: %s", response.Body.String())
	}
}

type statusOnlyWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusOnlyWriter) Status() int {
	return w.status
}

func (w *statusOnlyWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func TestRecoveryUsesStatusCapabilityWithoutConcreteType(t *testing.T) {
	t.Parallel()

	wrap := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(&statusOnlyWriter{ResponseWriter: w}, r)
		})
	}
	handler := wrap(withRecovery(discardLogger(), logging.Limits{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		panic("boom after write")
	})))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/status-only", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(response.Body.String(), "Internal Server Error") {
		t.Fatalf("recovery wrote a second response through a Status wrapper: %s", response.Body.String())
	}
}

func TestStatusRecorderCanBeUnwrapped(t *testing.T) {
	t.Parallel()

	underlying := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: underlying}
	unwrapper, ok := any(recorder).(interface{ Unwrap() http.ResponseWriter })
	if !ok {
		t.Fatal("statusRecorder does not implement Unwrap")
	}
	if got := unwrapper.Unwrap(); got != underlying {
		t.Fatal("Unwrap returned a different ResponseWriter")
	}
}

type writeHeaderCounter struct {
	headerCalls int
	status      int
}

func (w *writeHeaderCounter) Header() http.Header { return make(http.Header) }
func (w *writeHeaderCounter) WriteHeader(status int) {
	w.headerCalls++
	w.status = status
}
func (w *writeHeaderCounter) Write(body []byte) (int, error) { return len(body), nil }

func TestStatusRecorderIgnoresSubsequentWriteHeader(t *testing.T) {
	t.Parallel()

	underlying := &writeHeaderCounter{}
	recorder := &statusRecorder{ResponseWriter: underlying}
	recorder.WriteHeader(http.StatusOK)
	recorder.WriteHeader(http.StatusInternalServerError)

	if recorder.status != http.StatusOK {
		t.Errorf("recorded status = %d, want %d", recorder.status, http.StatusOK)
	}
	if underlying.headerCalls != 1 || underlying.status != http.StatusOK {
		t.Errorf("underlying WriteHeader calls/status = %d/%d, want 1/%d", underlying.headerCalls, underlying.status, http.StatusOK)
	}
}

func TestWithRecoveryLogsRequestIDOnPanic(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})
	handler := withRequestID(withRecovery(logger, logging.Limits{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
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

func TestWithRequestIDReplacesInvalidOrOversizedHeader(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		incoming string
	}{
		{name: "oversized", incoming: strings.Repeat("a", maxRequestIDLength+1)},
		{name: "space", incoming: "bad value"},
		{name: "control", incoming: "control\nchar"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var contextID string
			handler := withRequestID(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
				contextID = reqctx.RequestID(request.Context())
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("X-Request-ID", testCase.incoming)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if got := response.Header().Get("X-Request-ID"); got == testCase.incoming || got == "" || got != contextID {
				t.Errorf("response/context request IDs = %q/%q, want a generated matching ID", got, contextID)
			}
		})
	}
}

func TestValidRequestIDAcceptsMaximumLength(t *testing.T) {
	valid := strings.Repeat("a", maxRequestIDLength)
	if got := validRequestID(valid); got != valid {
		t.Fatalf("validRequestID() = %q, want the %d-character ID", got, maxRequestIDLength)
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
