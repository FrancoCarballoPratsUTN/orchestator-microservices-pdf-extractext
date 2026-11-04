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

// decodeLogRecords decodes every JSON record of the buffer. A single request
// can produce more than one line: the panic record and the access log line.
func decodeLogRecords(t *testing.T, raw []byte) []map[string]any {
	t.Helper()

	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log record is not valid JSON: %v (%q)", err, line)
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		t.Fatal("expected a log record, got none")
	}
	return records
}

// findLogRecord returns the record with the given message.
func findLogRecord(t *testing.T, records []map[string]any, msg string) map[string]any {
	t.Helper()

	for _, record := range records {
		if record["msg"] == msg {
			return record
		}
	}
	t.Fatalf("no log record with msg %q in %v", msg, records)
	return nil
}

func TestRequestLogRecordsStatus(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})

	handler := withRequestLog(logger, logging.Limits{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	if got := record["method"]; got != http.MethodGet {
		t.Errorf("method = %v, want %q", got, http.MethodGet)
	}
	if got := record["path"]; got != "/api/v1/audit/logs" {
		t.Errorf("path = %v, want %q", got, "/api/v1/audit/logs")
	}
	if got := record["level"]; got != "INFO" {
		t.Errorf("level = %v, want INFO", got)
	}
}

func TestRequestLogRecordsServerErrorAtErrorLevel(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})

	handler := withRequestLog(logger, logging.Limits{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	handler := withRequestLog(logger, logging.Limits{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

			handler := withRequestLog(logger, logging.Limits{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))

			if output.Len() != 0 {
				t.Errorf("access log for %s = %q, want no output", path, output.String())
			}
		})
	}
}

// Un POST a una sonda no es una sonda: es una petición equivocada y sí tiene
// que quedar registrada.
func TestRequestLogRecordsAMethodThatIsNotAProbe(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})

	handler := withRequestLog(logger, logging.Limits{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/healthz", nil))

	record := decodeLogRecord(t, output.Bytes())
	if got := record["status"]; got != float64(http.StatusMethodNotAllowed) {
		t.Errorf("status = %v, want %d", got, http.StatusMethodNotAllowed)
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

// A panic is also a request: it has to appear in the access log with its 500,
// or the audit trail of the service would be missing exactly the failures.
func TestAccessLogRecordsAPanickingRequest(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})

	handler := withRequestLog(logger, logging.Limits{})(withRecovery(logger, logging.Limits{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/pdfs/extract", nil))

	if response.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}

	records := decodeLogRecords(t, output.Bytes())
	access := findLogRecord(t, records, "http request")
	if got := access["status"]; got != float64(http.StatusInternalServerError) {
		t.Errorf("access log status = %v, want %d", got, http.StatusInternalServerError)
	}
}

// A response that already started cannot be rewritten: a second document would
// leave the client with a corrupted body and a status that lies.
func TestRecoveryDoesNotCorruptAResponseThatAlreadyStarted(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})

	handler := withRequestLog(logger, logging.Limits{})(withRecovery(logger, logging.Limits{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":"partial`))
		panic("boom")
	})))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/pdfs/extract", nil))

	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(response.Body.String(), "Internal Server Error") {
		t.Errorf("the body was corrupted by a second document: %s", response.Body.String())
	}
}

// La ruta y el método vienen del cliente: sin un tope, un path largo
// convertiría la línea de log en un archivo.
func TestAccessLogBoundsThePathAndTheMethod(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})

	handler := withRequestLog(logger, logging.Limits{MaxValueLength: 256})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	longPath := "/" + strings.Repeat("x", 256*2)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, longPath, nil))

	record := decodeLogRecord(t, output.Bytes())
	path, _ := record["path"].(string)
	if got := len([]rune(path)); got > 256+1 {
		t.Errorf("the path has %d runes, want it bounded to %d", got, 256+1)
	}
}
