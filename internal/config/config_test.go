package config

import (
	"testing"
	"time"
)

func envWith(values map[string]string) func(string) string {
	return func(key string) string {
		if value, ok := values[key]; ok {
			return value
		}
		return ""
	}
}

func TestLoadReturnsDefaultsWhenEnvironmentIsEmpty(t *testing.T) {
	t.Parallel()

	cfg, err := fromEnv(envWith(nil))

	if err != nil {
		t.Fatalf("fromEnv() unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.ExtractBaseURL != "http://localhost:8081" {
		t.Errorf("ExtractBaseURL = %q, want %q", cfg.ExtractBaseURL, "http://localhost:8081")
	}
	if cfg.PersistenceBaseURL != "http://localhost:8082" {
		t.Errorf("PersistenceBaseURL = %q, want %q", cfg.PersistenceBaseURL, "http://localhost:8082")
	}
	if cfg.AuditLogBaseURL != "http://localhost:8083" {
		t.Errorf("AuditLogBaseURL = %q, want %q", cfg.AuditLogBaseURL, "http://localhost:8083")
	}
	if cfg.HTTPTimeout != 10*time.Second {
		t.Errorf("HTTPTimeout = %v, want 10s", cfg.HTTPTimeout)
	}
	if cfg.MaxPDFSizeBytes != 15*1024*1024 {
		t.Errorf("MaxPDFSizeBytes = %d, want %d", cfg.MaxPDFSizeBytes, 15*1024*1024)
	}
	if cfg.MaxTextBodyBytes != 15*1024*1024 {
		t.Errorf("MaxTextBodyBytes = %d, want %d", cfg.MaxTextBodyBytes, 15*1024*1024)
	}
}

func TestLoadReadsEnvironmentOverrides(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"PORT":                 "9090",
		"EXTRACT_BASE_URL":     "http://extract-svc:4000",
		"PERSISTENCE_BASE_URL": "http://persistence-svc:4001",
		"AUDIT_LOG_BASE_URL":   "http://audit-svc:4002",
		"HTTP_TIMEOUT":         "30s",
		"MAX_PDF_SIZE_BYTES":   "20MB",
		"MAX_TEXT_BODY_BYTES":  "8MB",
	}

	cfg, err := fromEnv(envWith(env))

	if err != nil {
		t.Fatalf("fromEnv() unexpected error: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want %q", cfg.Port, "9090")
	}
	if cfg.ExtractBaseURL != "http://extract-svc:4000" {
		t.Errorf("ExtractBaseURL = %q, want %q", cfg.ExtractBaseURL, "http://extract-svc:4000")
	}
	if cfg.PersistenceBaseURL != "http://persistence-svc:4001" {
		t.Errorf("PersistenceBaseURL = %q, want %q", cfg.PersistenceBaseURL, "http://persistence-svc:4001")
	}
	if cfg.AuditLogBaseURL != "http://audit-svc:4002" {
		t.Errorf("AuditLogBaseURL = %q, want %q", cfg.AuditLogBaseURL, "http://audit-svc:4002")
	}
	if cfg.HTTPTimeout != 30*time.Second {
		t.Errorf("HTTPTimeout = %v, want 30s", cfg.HTTPTimeout)
	}
	if cfg.MaxPDFSizeBytes != 20*1024*1024 {
		t.Errorf("MaxPDFSizeBytes = %d, want %d", cfg.MaxPDFSizeBytes, 20*1024*1024)
	}
	if cfg.MaxTextBodyBytes != 8*1024*1024 {
		t.Errorf("MaxTextBodyBytes = %d, want %d", cfg.MaxTextBodyBytes, 8*1024*1024)
	}
}

func TestLoadRejectsInvalidHTTPTimeout(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{"HTTP_TIMEOUT": "not-a-duration"}))

	if err == nil {
		t.Fatal("fromEnv() expected an error for an invalid HTTP_TIMEOUT")
	}
}

func TestLoadRejectsInvalidMaxPDFSize(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{"MAX_PDF_SIZE_BYTES": "20GBs"}))

	if err == nil {
		t.Fatal("fromEnv() expected an error for an invalid MAX_PDF_SIZE_BYTES")
	}
}

func TestLoadRejectsNegativeMaxPDFSize(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{"MAX_PDF_SIZE_BYTES": "-1"}))

	if err == nil {
		t.Fatal("fromEnv() expected an error for a negative MAX_PDF_SIZE_BYTES")
	}
}
