package config

import (
	"strings"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/logging"
)

var testConfigValues = map[string]string{
	"PORT": "8080", "EXTRACT_BASE_URL": "http://localhost:8080",
	"PERSISTENCE_BASE_URL": "http://localhost:8000", "AUDIT_LOG_BASE_URL": "http://localhost:8083",
	"HTTP_TIMEOUT": "35s", "SHUTDOWN_TIMEOUT": "35s", "MAX_PDF_SIZE_BYTES": "15MB",
	"MAX_TEXT_BODY_BYTES": "15MB", "MAX_PDF_PAGES": "1000",
	"EXTRACT_HEALTH_URL":     "http://localhost:8080/healthz",
	"PERSISTENCE_HEALTH_URL": "http://localhost:8000/readyz",
	"AUDIT_LOG_HEALTH_URL":   "http://localhost:8083/readyz", "HEALTH_CHECK_TIMEOUT": "2s",
	"LOG_MAX_VALUE_LENGTH": "256", "LOG_MAX_STACK_LENGTH": "4096",
}

func envWith(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func testEnvWith(overrides map[string]string) func(string) string {
	values := testConfigMap(overrides)
	return func(key string) string {
		return values[key]
	}
}

func setConfigEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range testConfigValues {
		t.Setenv(key, value)
	}
}

func testConfigMap(overrides map[string]string) map[string]string {
	values := make(map[string]string, len(testConfigValues)+len(overrides))
	for key, value := range testConfigValues {
		values[key] = value
	}
	for key, value := range overrides {
		values[key] = value
	}
	return values
}

func TestLoadParsesRequiredSettings(t *testing.T) {
	t.Parallel()

	cfg, err := fromEnv(testEnvWith(map[string]string{
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

func TestLoadRejectsMissingPortInsteadOfUsingCodeDefault(t *testing.T) {
	_, err := fromEnv(func(key string) string {
		if key == "AUDIT_LOG_API_TOKEN" || key == "PERSISTENCE_API_TOKEN" {
			return "test-token"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "PORT") {
		t.Fatalf("fromEnv() error = %v, want missing PORT", err)
	}
}

func TestLoadFromValuesUsesConsulValues(t *testing.T) {
	t.Parallel()

	cfg, err := LoadFromValues(testConfigMap(map[string]string{
		"PORT":                  "9090",
		"EXTRACT_BASE_URL":      "https://extract:8443",
		"PERSISTENCE_BASE_URL":  "https://traefik:8443",
		"AUDIT_LOG_BASE_URL":    "https://traefik:8443",
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
		"HTTP_TIMEOUT":          "20s",
		"MAX_PDF_PAGES":         "50",
		"LOG_MAX_VALUE_LENGTH":  "180",
		"LOG_MAX_STACK_LENGTH":  "2048",
	}))
	if err != nil {
		t.Fatalf("LoadFromValues() unexpected error: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want 9090", cfg.Port)
	}
	if cfg.HTTPTimeout != 20*time.Second {
		t.Errorf("HTTPTimeout = %v, want 20s", cfg.HTTPTimeout)
	}
	if cfg.MaxPDFPages != 50 {
		t.Errorf("MaxPDFPages = %d, want 50", cfg.MaxPDFPages)
	}
	if cfg.LogMaxValueLength != 180 || cfg.LogMaxStackLength != 2048 {
		t.Errorf("log limits = (%d, %d), want (180, 2048)", cfg.LogMaxValueLength, cfg.LogMaxStackLength)
	}
}

func TestLoadRequiresLogLimits(t *testing.T) {
	values := testConfigMap(nil)
	values["AUDIT_LOG_API_TOKEN"] = "audit-token"
	values["PERSISTENCE_API_TOKEN"] = "persistence-token"
	delete(values, "LOG_MAX_VALUE_LENGTH")
	_, err := fromEnv(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "LOG_MAX_VALUE_LENGTH") {
		t.Fatalf("fromEnv() error = %v, want missing LOG_MAX_VALUE_LENGTH", err)
	}
}

func TestLoadDefaultsStackLimitWhenMissing(t *testing.T) {
	values := testConfigMap(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-token",
		"PERSISTENCE_API_TOKEN": "persistence-token",
	})
	delete(values, "LOG_MAX_STACK_LENGTH")

	cfg, err := fromEnv(envWith(values))
	if err != nil {
		t.Fatalf("fromEnv() unexpected error: %v", err)
	}
	if cfg.LogMaxStackLength != logging.DefaultMaxStackLength {
		t.Errorf("LogMaxStackLength = %d, want %d", cfg.LogMaxStackLength, logging.DefaultMaxStackLength)
	}
}

func TestLogLimitsReflectsConfiguredValues(t *testing.T) {
	t.Parallel()

	cfg := Config{LogMaxValueLength: 180, LogMaxStackLength: 2048}

	limits := cfg.LogLimits()

	if limits.MaxValueLength != 180 || limits.MaxStackLength != 2048 {
		t.Errorf("LogLimits() = (%d, %d), want (180, 2048)",
			limits.MaxValueLength, limits.MaxStackLength)
	}
}

func TestLogLimitsFallsBackToSafeDefaults(t *testing.T) {
	t.Parallel()

	limits := Config{}.LogLimits()

	if got := limits.ValueLength(); got != logging.DefaultMaxValueLength {
		t.Errorf("ValueLength() = %d, want %d", got, logging.DefaultMaxValueLength)
	}
	if got := limits.StackLength(); got != logging.DefaultMaxStackLength {
		t.Errorf("StackLength() = %d, want %d", got, logging.DefaultMaxStackLength)
	}
}

func TestLoadFromValuesFallsBackToHealthURLsFromEnvironment(t *testing.T) {
	setConfigEnvironment(t)
	t.Setenv("LOG_MAX_VALUE_LENGTH", "256")
	t.Setenv("LOG_MAX_STACK_LENGTH", "4096")
	t.Setenv("EXTRACT_HEALTH_URL", "http://app:8080/healthz")
	t.Setenv("PERSISTENCE_HEALTH_URL", "http://persistence:8000/readyz")
	t.Setenv("AUDIT_LOG_HEALTH_URL", "http://audit:8083/readyz")

	cfg, err := LoadFromValues(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
	})
	if err != nil {
		t.Fatalf("LoadFromValues() unexpected error: %v", err)
	}

	if cfg.ExtractHealthURL != "http://app:8080/healthz" {
		t.Errorf("ExtractHealthURL = %q, want the environment value", cfg.ExtractHealthURL)
	}
	if cfg.PersistenceHealthURL != "http://persistence:8000/readyz" {
		t.Errorf("PersistenceHealthURL = %q, want the environment value", cfg.PersistenceHealthURL)
	}
	if cfg.AuditLogHealthURL != "http://audit:8083/readyz" {
		t.Errorf("AuditLogHealthURL = %q, want the environment value", cfg.AuditLogHealthURL)
	}
}

func TestLoadFromValuesPrefersConsulOverEnvironment(t *testing.T) {
	setConfigEnvironment(t)
	t.Setenv("LOG_MAX_VALUE_LENGTH", "256")
	t.Setenv("LOG_MAX_STACK_LENGTH", "4096")
	t.Setenv("EXTRACT_HEALTH_URL", "http://environment:8080/healthz")

	cfg, err := LoadFromValues(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
		"EXTRACT_HEALTH_URL":    "http://consul:8080/healthz",
	})
	if err != nil {
		t.Fatalf("LoadFromValues() unexpected error: %v", err)
	}
	if cfg.ExtractHealthURL != "http://consul:8080/healthz" {
		t.Errorf("ExtractHealthURL = %q, want the Consul value", cfg.ExtractHealthURL)
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
		"EXTRACT_HEALTH_URL":     "http://extract-svc:4000/healthz",
		"PERSISTENCE_HEALTH_URL": "http://persistence-svc:4001/readyz",
		"AUDIT_LOG_HEALTH_URL":   "http://audit-svc:4002/readyz",
		"HEALTH_CHECK_TIMEOUT":   "1500ms",
	}

	cfg, err := fromEnv(testEnvWith(env))

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

	_, err := fromEnv(testEnvWith(map[string]string{"PERSISTENCE_API_TOKEN": "persistence-secret"}))

	if err == nil {
		t.Fatal("fromEnv() expected an error when AUDIT_LOG_API_TOKEN is missing")
	}
}

func TestLoadRejectsBlankAuditLogAPIToken(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(testEnvWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "   ",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
	}))

	if err == nil {
		t.Fatal("fromEnv() expected an error when AUDIT_LOG_API_TOKEN is blank")
	}
}

func TestLoadRequiresPersistenceAPIToken(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(testEnvWith(map[string]string{"AUDIT_LOG_API_TOKEN": "audit-secret"}))

	if err == nil {
		t.Fatal("fromEnv() expected an error when PERSISTENCE_API_TOKEN is missing")
	}
}

func TestLoadRejectsBlankPersistenceAPIToken(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(testEnvWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "  ",
	}))

	if err == nil {
		t.Fatal("fromEnv() expected an error when PERSISTENCE_API_TOKEN is blank")
	}
}

func TestLoadRejectsInvalidHTTPTimeout(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(testEnvWith(map[string]string{
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
		_, err := fromEnv(testEnvWith(map[string]string{
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

	_, err := fromEnv(testEnvWith(map[string]string{
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

	_, err := fromEnv(testEnvWith(map[string]string{
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

	_, err := fromEnv(testEnvWith(map[string]string{
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

	_, err := fromEnv(testEnvWith(map[string]string{
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

	_, err := fromEnv(testEnvWith(map[string]string{
		"EXTRACT_BASE_URL":      "extract:8080",
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
	}))
	if err == nil {
		t.Fatal("fromEnv() expected an error for an invalid EXTRACT_BASE_URL")
	}
}

func TestValidateBaseURLDoesNotEchoCredentialsOrRaw(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
	}{
		{"relative", "/relative/path"},
		{"no scheme", "localhost:8080"},
		{"no host", "http:///no-host"},
		{"unsupported scheme", "ftp://example.com"},
		{"credentials without host", "http://user:secret@/x"},
		{"empty", ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := baseEnv()
			env["EXTRACT_BASE_URL"] = tc.url
			_, err := fromEnv(testEnvWith(env))
			if err == nil {
				t.Fatalf("fromEnv() expected an error for URL %q", tc.url)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Errorf("error leaked credential %q: %v", "secret", err)
			}
			if tc.url != "" && strings.Contains(err.Error(), tc.url) {
				t.Errorf("error contains raw url %q: %v", tc.url, err)
			}
		})
	}
}

func TestLoadRejectsNegativeMaxPDFSize(t *testing.T) {
	t.Parallel()

	_, err := fromEnv(testEnvWith(map[string]string{
		"AUDIT_LOG_API_TOKEN":   "audit-secret",
		"PERSISTENCE_API_TOKEN": "persistence-secret",
		"MAX_PDF_SIZE_BYTES":    "-1",
	}))

	if err == nil {
		t.Fatal("fromEnv() expected an error for a negative MAX_PDF_SIZE_BYTES")
	}
}

func TestLoadRejectsZeroPayloadLimits(t *testing.T) {
	for _, key := range []string{"MAX_PDF_SIZE_BYTES", "MAX_TEXT_BODY_BYTES"} {
		t.Run(key, func(t *testing.T) {
			values := testConfigMap(map[string]string{
				"AUDIT_LOG_API_TOKEN":   "audit-token",
				"PERSISTENCE_API_TOKEN": "persistence-token",
				key:                     "0",
			})
			if _, err := fromEnv(envWith(values)); err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("fromEnv() error = %v, want positive %s", err, key)
			}
		})
	}
}

func TestLoadAcceptsZeroMaxPDFPagesAsUnlimited(t *testing.T) {
	t.Parallel()

	cfg, err := fromEnv(testEnvWith(map[string]string{
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

	_, err := fromEnv(testEnvWith(map[string]string{
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

	_, err := fromEnv(testEnvWith(map[string]string{
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

func TestLoadRejectsInvalidHealthCheckTimeout(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"not-a-duration", "0s", "-1s"} {
		env := baseEnv()
		env["HEALTH_CHECK_TIMEOUT"] = raw
		if _, err := fromEnv(testEnvWith(env)); err == nil {
			t.Errorf("fromEnv() expected an error for HEALTH_CHECK_TIMEOUT=%q", raw)
		}
	}
}

func TestLoadRejectsInvalidHealthURL(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"EXTRACT_HEALTH_URL", "PERSISTENCE_HEALTH_URL", "AUDIT_LOG_HEALTH_URL"} {
		env := baseEnv()
		env[key] = "extract:8080/healthz"
		if _, err := fromEnv(testEnvWith(env)); err == nil {
			t.Errorf("fromEnv() expected an error for an invalid %s", key)
		}
	}
}

func TestParsersAcceptWhitespace(t *testing.T) {
	t.Parallel()

	env := baseEnv()
	env["PORT"] = "  8080  "
	env["HTTP_TIMEOUT"] = "  35s  "
	env["SHUTDOWN_TIMEOUT"] = "  5s  "
	env["MAX_PDF_SIZE_BYTES"] = "  15MB  "
	env["MAX_TEXT_BODY_BYTES"] = "  15MB  "
	env["MAX_PDF_PAGES"] = "  1000  "

	cfg, err := fromEnv(testEnvWith(env))
	if err != nil {
		t.Fatalf("fromEnv() unexpected error with whitespace: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.HTTPTimeout != 35*time.Second {
		t.Errorf("HTTPTimeout = %v, want 35s", cfg.HTTPTimeout)
	}
	if cfg.MaxPDFSizeBytes != 15*1024*1024 {
		t.Errorf("MaxPDFSizeBytes = %d, want 15MB", cfg.MaxPDFSizeBytes)
	}
	if cfg.MaxPDFPages != 1000 {
		t.Errorf("MaxPDFPages = %d, want 1000", cfg.MaxPDFPages)
	}
}

func TestURLValidationOrderIsDeterministic(t *testing.T) {
	t.Parallel()

	for i := 0; i < 20; i++ {
		env := baseEnv()
		env["EXTRACT_BASE_URL"] = "invalid-url-1"
		env["PERSISTENCE_BASE_URL"] = "invalid-url-2"
		env["AUDIT_LOG_BASE_URL"] = "invalid-url-3"
		env["EXTRACT_HEALTH_URL"] = "invalid-url-4"
		env["PERSISTENCE_HEALTH_URL"] = "invalid-url-5"
		env["AUDIT_LOG_HEALTH_URL"] = "invalid-url-6"

		_, err := fromEnv(testEnvWith(env))
		if err == nil {
			t.Fatal("fromEnv() expected an error for invalid URLs")
		}
		if !strings.Contains(err.Error(), "EXTRACT_BASE_URL") {
			t.Fatalf("run %d: error = %v, want first error to be EXTRACT_BASE_URL", i, err)
		}
	}
}
