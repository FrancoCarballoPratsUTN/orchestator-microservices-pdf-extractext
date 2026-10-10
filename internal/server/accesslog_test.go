package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"validationmicroservices-pdf-extractext/internal/logging"
)

func decodeLogRecord(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	line := strings.TrimSpace(string(raw))
	if line == "" {
		t.Fatal("expected an access log record, got none")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("access log record is not valid JSON: %v (%q)", err, line)
	}
	return record
}

func TestRequestLogRecordsStatusBytesAndRemoteAddr(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})

	handler := withRequestLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit/logs", nil)
	request.RemoteAddr = "203.0.113.7:5555"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	record := decodeLogRecord(t, output.Bytes())
	if got := record["status"]; got != float64(http.StatusOK) {
		t.Errorf("status = %v, want %d", got, http.StatusOK)
	}
	if got := record["bytes"]; got != float64(len("hello")) {
		t.Errorf("bytes = %v, want %d", got, len("hello"))
	}
	if got := record["method"]; got != http.MethodGet {
		t.Errorf("method = %v, want %q", got, http.MethodGet)
	}
	if got := record["path"]; got != "/api/v1/audit/logs" {
		t.Errorf("path = %v, want %q", got, "/api/v1/audit/logs")
	}
	if got := record["remote_addr"]; got != "203.0.113.7:5555" {
		t.Errorf("remote_addr = %v, want %q", got, "203.0.113.7:5555")
	}
	if got := record["level"]; got != "INFO" {
		t.Errorf("level = %v, want INFO", got)
	}
}

func TestRequestLogRecordsServerErrorAtErrorLevel(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})

	handler := withRequestLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	request := httptest.NewRequest(http.MethodGet, "/boom", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	record := decodeLogRecord(t, output.Bytes())
	if got := record["level"]; got != "ERROR" {
		t.Errorf("level = %v, want ERROR", got)
	}
	if got := record["status"]; got != float64(http.StatusInternalServerError) {
		t.Errorf("status = %v, want %d", got, http.StatusInternalServerError)
	}
}

func TestRequestLogRecordsClientErrorAtWarnLevel(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})

	handler := withRequestLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	record := decodeLogRecord(t, output.Bytes())
	if got := record["level"]; got != "WARN" {
		t.Errorf("level = %v, want WARN", got)
	}
	if got := record["status"]; got != float64(http.StatusNotFound) {
		t.Errorf("status = %v, want %d", got, http.StatusNotFound)
	}
}

func TestRequestLogSkipsHealthAndReadinessEndpoints(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/healthz", "/readyz"} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})

			handler := withRequestLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))

			if output.Len() != 0 {
				t.Errorf("access log for %s = %q, want no output", path, output.String())
			}
		})
	}
}

func TestAccessLogLevel(t *testing.T) {
	t.Parallel()

	cases := map[int]slog.Level{
		200: slog.LevelInfo,
		301: slog.LevelInfo,
		404: slog.LevelWarn,
		422: slog.LevelWarn,
		500: slog.LevelError,
		503: slog.LevelError,
	}
	for status, want := range cases {
		status, want := status, want
		if got := accessLogLevel(status); got != want {
			t.Errorf("accessLogLevel(%d) = %v, want %v", status, got, want)
		}
	}
}
