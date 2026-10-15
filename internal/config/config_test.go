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

func TestLoadReturnsDefaultsForOptionalSettings(t *testing.T) {
	t.Parallel()

	cfg, err := fromEnv(envWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
	}))

	if err != nil {
		t.Fatalf("fromEnv() unexpected error: %v", err)
	}
	if cfg.AuditLogAPIToken != "audit-secret" {
		t.Errorf("AuditLogAPIToken = %q, want %q", cfg.AuditLogAPIToken, "audit-secret")
	}
	if cfg.PersistenceAPIToken != "persistence-secret" {
		t.Errorf("PersistenceAPIToken = %q, want %q", cfg.PersistenceAPIToken, "persistence-secret")
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	// Extract y AuditLog usan los puertos que declaran sus servicios reales
	// (extract.go / cmd de cada microservicio), no puertos secuenciales inventados.
	if cfg.ExtractBaseURL != "http://localhost:8080" {
		t.Errorf("ExtractBaseURL = %q, want %q", cfg.ExtractBaseURL, "http://localhost:8080")
	}
	if cfg.PersistenceBaseURL != "http://localhost:8000" {
		t.Errorf("PersistenceBaseURL = %q, want %q", cfg.PersistenceBaseURL, "http://localhost:8000")
	}
	if cfg.AuditLogBaseURL != "http://localhost:8083" {
		t.Errorf("AuditLogBaseURL = %q, want %q", cfg.AuditLogBaseURL, "http://localhost:8083")
	}
	// 35s y no 10s: el Extract tiene un deadline interno de 30s. Con 10s el
	// orquestador cortaría PDFs grandes que el Extract sí terminaría.
	if cfg.HTTPTimeout != 35*time.Second {
		t.Errorf("HTTPTimeout = %v, want 35s", cfg.HTTPTimeout)
	}
	if cfg.ShutdownTimeout != 35*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 35s", cfg.ShutdownTimeout)
	}
	if cfg.MaxPDFSizeBytes != 15*1024*1024 {
		t.Errorf("MaxPDFSizeBytes = %d, want %d", cfg.MaxPDFSizeBytes, 15*1024*1024)
	}
	if cfg.MaxTextBodyBytes != 15*1024*1024 {
		t.Errorf("MaxTextBodyBytes = %d, want %d", cfg.MaxTextBodyBytes, 15*1024*1024)
	}
	if cfg.MaxPDFPages != 1000 {
		t.Errorf("MaxPDFPages = %d, want 1000", cfg.MaxPDFPages)
	}
	if cfg.RetryMaxAttempts != 3 {
		t.Errorf("RetryMaxAttempts = %d, want 3", cfg.RetryMaxAttempts)
	}
	if cfg.RetryBackoffBase != 100*time.Millisecond {
		t.Errorf("RetryBackoffBase = %v, want 100ms", cfg.RetryBackoffBase)
	}
	if cfg.RetryBackoffMax != time.Second {
		t.Errorf("RetryBackoffMax = %v, want 1s", cfg.RetryBackoffMax)
	}
	if cfg.ExtractHealthURL != "http://localhost:8080/healthz" {
		t.Errorf("ExtractHealthURL = %q, want %q", cfg.ExtractHealthURL, "http://localhost:8080/healthz")
	}
	if cfg.PersistenceHealthURL != "http://localhost:8000/readyz" {
		t.Errorf("PersistenceHealthURL = %q, want %q", cfg.PersistenceHealthURL, "http://localhost:8000/readyz")
	}
	if cfg.AuditLogHealthURL != "http://localhost:8083/readyz" {
		t.Errorf("AuditLogHealthURL = %q, want %q", cfg.AuditLogHealthURL, "http://localhost:8083/readyz")
	}
	if cfg.HealthCheckTimeout != 2*time.Second {
		t.Errorf("HealthCheckTimeout = %v, want 2s", cfg.HealthCheckTimeout)
	}
}

func TestLoadReadsEnvironmentOverrides(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"PORT":                   "9090",
		"EXTRACT_BASE_URL":       "http://extract-svc:4000",
		"PERSISTENCE_BASE_URL":   "http://persistence-svc:4001",
		"AUDIT_LOG_BASE_URL":     "http://audit-svc:4002",
		"AUDIT_LOG_API_TOKEN":    "audit-secret",
		"PERSISTENCE_API_TOKEN":  "persistence-secret",
		"HTTP_TIMEOUT":           "30s",
		"SHUTDOWN_TIMEOUT":       "5s",
		"MAX_PDF_SIZE_BYTES":     "20MB",
		"MAX_TEXT_BODY_BYTES":    "8MB",
		"MAX_PDF_PAGES":          "50",
		"RETRY_MAX_ATTEMPTS":     "5",
		"RETRY_BACKOFF_BASE":     "50ms",
		"RETRY_BACKOFF_MAX":      "2s",
		"EXTRACT_HEALTH_URL":     "http://extract-svc:4000/healthz",
		"PERSISTENCE_HEALTH_URL": "http://persistence-svc:4001/readyz",
		"AUDIT_LOG_HEALTH_URL":   "http://audit-svc:4002/readyz",
		"HEALTH_CHECK_TIMEOUT":   "1500ms",
	}

	cfg, err := fromEnv(envWith(env))

	if err != nil {
		t.Fatalf("fromEnv() unexpected error: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want %q", cfg.Port, "9090")
	}
	if cfg.AuditLogAPIToken != "audit-secret" {
		t.Errorf("AuditLogAPIToken = %q, want %q", cfg.AuditLogAPIToken, "audit-secret")
	}
	if cfg.PersistenceAPIToken != "persistence-secret" {
		t.Errorf("PersistenceAPIToken = %q, want %q", cfg.PersistenceAPIToken, "persistence-secret")
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
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 5s", cfg.ShutdownTimeout)
	}
	if cfg.MaxPDFSizeBytes != 20*1024*1024 {
		t.Errorf("MaxPDFSizeBytes = %d, want %d", cfg.MaxPDFSizeBytes, 20*1024*1024)
	}
	if cfg.MaxTextBodyBytes != 8*1024*1024 {
		t.Errorf("MaxTextBodyBytes = %d, want %d", cfg.MaxTextBodyBytes, 8*1024*1024)
	}
	if cfg.MaxPDFPages != 50 {
		t.Errorf("MaxPDFPages = %d, want 50", cfg.MaxPDFPages)
	}
	if cfg.RetryMaxAttempts != 5 {
		t.Errorf("RetryMaxAttempts = %d, want 5", cfg.RetryMaxAttempts)
	}
	if cfg.RetryBackoffBase != 50*time.Millisecond {
		t.Errorf("RetryBackoffBase = %v, want 50ms", cfg.RetryBackoffBase)
	}
	if cfg.RetryBackoffMax != 2*time.Second {
		t.Errorf("RetryBackoffMax = %v, want 2s", cfg.RetryBackoffMax)
	}
	if cfg.ExtractHealthURL != "http://extract-svc:4000/healthz" {
		t.Errorf("ExtractHealthURL = %q, want %q", cfg.ExtractHealthURL, "http://extract-svc:4000/healthz")
	}
	if cfg.PersistenceHealthURL != "http://persistence-svc:4001/readyz" {
		t.Errorf("PersistenceHealthURL = %q, want %q", cfg.PersistenceHealthURL, "http://persistence-svc:4001/readyz")
	}
	if cfg.AuditLogHealthURL != "http://audit-svc:4002/readyz" {
		t.Errorf("AuditLogHealthURL = %q, want %q", cfg.AuditLogHealthURL, "http://audit-svc:4002/readyz")
	}
	if cfg.HealthCheckTimeout != 1500*time.Millisecond {
		t.Errorf("HealthCheckTimeout = %v, want 1500ms", cfg.HealthCheckTimeout)
	}
}

func TestLoadRequiresAuditLogAPIToken(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{"PERSISTENCE_API_TOKEN": "persistence-secret"}))

	if err == nil {
		t.Fatal("fromEnv() expected an error when AUDIT_LOG_API_TOKEN is missing")
	}
}

func TestLoadRejectsBlankAuditLogAPIToken(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "   ",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
	}))

	if err == nil {
		t.Fatal("fromEnv() expected an error when AUDIT_LOG_API_TOKEN is blank")
	}
}

func TestLoadRequiresPersistenceAPIToken(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{"AUDIT_LOG_API_TOKEN": "audit-secret"}))

	if err == nil {
		t.Fatal("fromEnv() expected an error when PERSISTENCE_API_TOKEN is missing")
	}
}

func TestLoadRejectsBlankPersistenceAPIToken(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "  ",
	}))

	if err == nil {
		t.Fatal("fromEnv() expected an error when PERSISTENCE_API_TOKEN is blank")
	}
}

func TestLoadRejectsInvalidHTTPTimeout(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
		"HTTP_TIMEOUT":          "not-a-duration",
	}))

	if err == nil {
		t.Fatal("fromEnv() expected an error for an invalid HTTP_TIMEOUT")
	}
}

func TestLoadRejectsNonPositiveHTTPTimeout(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"0s", "-1s"} {
		_, err := fromEnv(envWith(map[string]string{
			"AUDIT_LOG_API_TOKEN":   "audit-secret",
			"PERSISTENCE_API_TOKEN": "persistence-secret",
			"HTTP_TIMEOUT":          raw,
		}))
		if err == nil {
			t.Errorf("fromEnv() expected an error for HTTP_TIMEOUT=%q", raw)
		}
	}
}

func TestLoadRejectsInvalidShutdownTimeout(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
		"SHUTDOWN_TIMEOUT":      "not-a-duration",
	}))

	if err == nil {
		t.Fatal("fromEnv() expected an error for an invalid SHUTDOWN_TIMEOUT")
	}
}

func TestLoadRejectsInvalidMaxPDFSize(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
		"MAX_PDF_SIZE_BYTES":    "20GBs",
	}))

	if err == nil {
		t.Fatal("fromEnv() expected an error for an invalid MAX_PDF_SIZE_BYTES")
	}
}

func TestLoadRejectsOverflowingMaxPDFSize(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
		"MAX_PDF_SIZE_BYTES":    "9223372036854775807KB",
	}))
	if err == nil {
		t.Fatal("fromEnv() expected an error for an overflowing MAX_PDF_SIZE_BYTES")
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{
		"PORT":                  "70000",
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
	}))
	if err == nil {
		t.Fatal("fromEnv() expected an error for an invalid PORT")
	}
}

func TestLoadRejectsInvalidBaseURL(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{
		"EXTRACT_BASE_URL":      "extract:8080",
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
	}))
	if err == nil {
		t.Fatal("fromEnv() expected an error for an invalid EXTRACT_BASE_URL")
	}
}

func TestLoadRejectsNegativeMaxPDFSize(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
		"MAX_PDF_SIZE_BYTES":    "-1",
	}))

	if err == nil {
		t.Fatal("fromEnv() expected an error for a negative MAX_PDF_SIZE_BYTES")
	}
}

func TestLoadAcceptsZeroMaxPDFPagesAsUnlimited(t *testing.T) {
	t.Parallel()

	cfg, err := fromEnv(envWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
		"MAX_PDF_PAGES":         "0",
	}))

	if err != nil {
		t.Fatalf("fromEnv() unexpected error: %v", err)
	}
	if cfg.MaxPDFPages != 0 {
		t.Errorf("MaxPDFPages = %d, want 0 (no limit)", cfg.MaxPDFPages)
	}
}

func TestLoadRejectsInvalidMaxPDFPages(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
		"MAX_PDF_PAGES":         "muchas",
	}))

	if err == nil {
		t.Fatal("fromEnv() expected an error for a non-numeric MAX_PDF_PAGES")
	}
}

func TestLoadRejectsNegativeMaxPDFPages(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(envWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
		"MAX_PDF_PAGES":         "-1",
	}))

	if err == nil {
		t.Fatal("fromEnv() expected an error for a negative MAX_PDF_PAGES")
	}
}

func baseEnv() map[string]string {
	return map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
	}
}

func TestLoadRejectsNonNumericRetryMaxAttempts(t *testing.T) {
	t.Parallel()

	env := baseEnv()
	env["RETRY_MAX_ATTEMPTS"] = "muchos"
	if _, err := fromEnv(envWith(env)); err == nil {
		t.Fatal("fromEnv() expected an error for a non-numeric RETRY_MAX_ATTEMPTS")
	}
}

func TestLoadRejectsNonPositiveRetryMaxAttempts(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"0", "-1"} {
		env := baseEnv()
		env["RETRY_MAX_ATTEMPTS"] = raw
		if _, err := fromEnv(envWith(env)); err == nil {
			t.Errorf("fromEnv() expected an error for RETRY_MAX_ATTEMPTS=%q", raw)
		}
	}
}

func TestLoadRejectsInvalidRetryBackoffBase(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"not-a-duration", "0s", "-1s"} {
		env := baseEnv()
		env["RETRY_BACKOFF_BASE"] = raw
		if _, err := fromEnv(envWith(env)); err == nil {
			t.Errorf("fromEnv() expected an error for RETRY_BACKOFF_BASE=%q", raw)
		}
	}
}

func TestLoadRejectsInvalidRetryBackoffMax(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"not-a-duration", "0s", "-1s"} {
		env := baseEnv()
		env["RETRY_BACKOFF_MAX"] = raw
		if _, err := fromEnv(envWith(env)); err == nil {
			t.Errorf("fromEnv() expected an error for RETRY_BACKOFF_MAX=%q", raw)
		}
	}
}

func TestLoadRejectsInvalidHealthCheckTimeout(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"not-a-duration", "0s", "-1s"} {
		env := baseEnv()
		env["HEALTH_CHECK_TIMEOUT"] = raw
		if _, err := fromEnv(envWith(env)); err == nil {
			t.Errorf("fromEnv() expected an error for HEALTH_CHECK_TIMEOUT=%q", raw)
		}
	}
}

func TestLoadRejectsInvalidHealthURL(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"EXTRACT_HEALTH_URL", "PERSISTENCE_HEALTH_URL", "AUDIT_LOG_HEALTH_URL"} {
		env := baseEnv()
		env[key] = "extract:8080/healthz"
		if _, err := fromEnv(envWith(env)); err == nil {
			t.Errorf("fromEnv() expected an error for an invalid %s", key)
		}
	}
}
