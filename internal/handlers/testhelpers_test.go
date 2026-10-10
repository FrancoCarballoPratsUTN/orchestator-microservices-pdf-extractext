package handlers

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"validationmicroservices-pdf-extractext/internal/logging"
	"validationmicroservices-pdf-extractext/internal/services"
)

func discardLogger() *slog.Logger {
	return logging.Discard()
}

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var output bytes.Buffer
	logger := logging.NewWithOptions(logging.Options{Level: slog.LevelDebug, Output: &output})
	return logger, &output
}

func decodeLogRecord(t *testing.T, data []byte) map[string]any {
	t.Helper()
	line := bytes.TrimSpace(data)
	if len(line) == 0 {
		t.Fatal("no log output was produced")
	}
	var record map[string]any
	if err := json.Unmarshal(line, &record); err != nil {
		t.Fatalf("log output is not valid JSON: %v (%q)", err, line)
	}
	return record
}

func newTestPDFHandler(service services.PDFService, maxPDFSize int64) *PDFHandler {
	return NewPDFHandler(service, maxPDFSize, discardLogger())
}

func newTestTextHandler(service services.TextService, maxTextBodyBytes int64) *TextHandler {
	return NewTextHandler(service, maxTextBodyBytes, discardLogger())
}

func newTestAuditHandler(service services.AuditService) *AuditHandler {
	return NewAuditHandler(service, discardLogger())
}
