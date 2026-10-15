package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPort = "8080"

	// defaultExtractBaseURL usa el puerto que el MS Extract declara (8080). El
	// default anterior era 8081, que en realidad es el de AuditLog.
	defaultExtractBaseURL = "http://localhost:8080"
	// defaultPersistenceBaseURL usa el puerto real del MS Persistence (8000).
	defaultPersistenceBaseURL = "http://localhost:8000"
	// defaultAuditLogBaseURL usa el puerto real de AUDA (8083), no el 8081 de un
	// EXPOSE viejo.
	defaultAuditLogBaseURL = "http://localhost:8083"

	// defaultHTTPTimeout supera el deadline interno del Extract (30s) para que
	// el corte lo decida el Extract y no el cliente. Con 10s el orquestador
	// abandonaba PDFs grandes que el Extract sí habría terminado.
	//
	// Fase 12: es el PRESUPUESTO GLOBAL de una llamada E-W (todos los intentos
	// de retry caben dentro de este deadline), no el timeout por intento. El
	// corte lo impone un context.WithTimeout, no el Timeout del http.Client.
	defaultHTTPTimeout = 35 * time.Second

	// defaultShutdownTimeout cubre una request en vuelo que usa todo el
	// HTTP_TIMEOUT: si el apagado cortara antes, abandonaría una extracción que
	// sí iba a terminar. Bajarla por entorno habilita un drenado más agresivo.
	defaultShutdownTimeout = 35 * time.Second

	defaultMaxPDFSizeBytes  = int64(15 * 1024 * 1024)
	defaultMaxTextBodyBytes = defaultMaxPDFSizeBytes

	// defaultMaxPDFPages acota el trabajo del Extract. Cero significa sin límite;
	// el default corta PDFs patológicos sin molestar al uso normal.
	defaultMaxPDFPages = 1000

	// Retry de las llamadas E-W. El default global son 3 intentos con backoff
	// exponencial con jitter; cada cliente puede fijar su propia política.
	defaultRetryMaxAttempts = 3
	defaultRetryBackoffBase = 100 * time.Millisecond
	defaultRetryBackoffMax  = time.Second

	// Health de los MS, consultado por /readyz. Por DNS directo en la red
	// interna (independiente del circuit breaker de Traefik). Extract expone
	// /healthz; Persistence y Audit /readyz (que sí chequean Mongo).
	defaultExtractHealthURL     = "http://localhost:8080/healthz"
	defaultPersistenceHealthURL = "http://localhost:8000/readyz"
	defaultAuditLogHealthURL    = "http://localhost:8083/readyz"
	defaultHealthCheckTimeout   = 2 * time.Second
)

type Config struct {
	Port                 string
	ExtractBaseURL       string
	PersistenceBaseURL   string
	AuditLogBaseURL      string
	AuditLogAPIToken     string
	PersistenceAPIToken  string
	HTTPTimeout          time.Duration
	ShutdownTimeout      time.Duration
	MaxPDFSizeBytes      int64
	MaxTextBodyBytes     int64
	MaxPDFPages          int
	RetryMaxAttempts     int
	RetryBackoffBase     time.Duration
	RetryBackoffMax      time.Duration
	ExtractHealthURL     string
	PersistenceHealthURL string
	AuditLogHealthURL    string
	HealthCheckTimeout   time.Duration
}

func Load() (Config, error) {
	return fromEnv(os.Getenv)
}

func fromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		Port:                 envOrDefault(getenv, "PORT", defaultPort),
		ExtractBaseURL:       envOrDefault(getenv, "EXTRACT_BASE_URL", defaultExtractBaseURL),
		PersistenceBaseURL:   envOrDefault(getenv, "PERSISTENCE_BASE_URL", defaultPersistenceBaseURL),
		AuditLogBaseURL:      envOrDefault(getenv, "AUDIT_LOG_BASE_URL", defaultAuditLogBaseURL),
		HTTPTimeout:          defaultHTTPTimeout,
		ShutdownTimeout:      defaultShutdownTimeout,
		MaxPDFSizeBytes:      defaultMaxPDFSizeBytes,
		MaxTextBodyBytes:     defaultMaxTextBodyBytes,
		MaxPDFPages:          defaultMaxPDFPages,
		RetryMaxAttempts:     defaultRetryMaxAttempts,
		RetryBackoffBase:     defaultRetryBackoffBase,
		RetryBackoffMax:      defaultRetryBackoffMax,
		ExtractHealthURL:     envOrDefault(getenv, "EXTRACT_HEALTH_URL", defaultExtractHealthURL),
		PersistenceHealthURL: envOrDefault(getenv, "PERSISTENCE_HEALTH_URL", defaultPersistenceHealthURL),
		AuditLogHealthURL:    envOrDefault(getenv, "AUDIT_LOG_HEALTH_URL", defaultAuditLogHealthURL),
		HealthCheckTimeout:   defaultHealthCheckTimeout,
	}

	if err := validatePort("PORT", cfg.Port); err != nil {
		return Config{}, err
	}
	for name, value := range map[string]string{
		"EXTRACT_BASE_URL":       cfg.ExtractBaseURL,
		"PERSISTENCE_BASE_URL":   cfg.PersistenceBaseURL,
		"AUDIT_LOG_BASE_URL":     cfg.AuditLogBaseURL,
		"EXTRACT_HEALTH_URL":     cfg.ExtractHealthURL,
		"PERSISTENCE_HEALTH_URL": cfg.PersistenceHealthURL,
		"AUDIT_LOG_HEALTH_URL":   cfg.AuditLogHealthURL,
	} {
		if err := validateBaseURL(name, value); err != nil {
			return Config{}, err
		}
	}

	timeout, err := parseDuration("HTTP_TIMEOUT", getenv("HTTP_TIMEOUT"), defaultHTTPTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.HTTPTimeout = timeout

	shutdownTimeout, err := parseDuration("SHUTDOWN_TIMEOUT", getenv("SHUTDOWN_TIMEOUT"), defaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.ShutdownTimeout = shutdownTimeout

	size, err := parseByteSize("MAX_PDF_SIZE_BYTES", getenv("MAX_PDF_SIZE_BYTES"), defaultMaxPDFSizeBytes)
	if err != nil {
		return Config{}, err
	}
	cfg.MaxPDFSizeBytes = size

	textSize, err := parseByteSize("MAX_TEXT_BODY_BYTES", getenv("MAX_TEXT_BODY_BYTES"), defaultMaxTextBodyBytes)
	if err != nil {
		return Config{}, err
	}
	cfg.MaxTextBodyBytes = textSize

	pages, err := parsePageLimit("MAX_PDF_PAGES", getenv("MAX_PDF_PAGES"), defaultMaxPDFPages)
	if err != nil {
		return Config{}, err
	}
	cfg.MaxPDFPages = pages

	attempts, err := parsePositiveInt("RETRY_MAX_ATTEMPTS", getenv("RETRY_MAX_ATTEMPTS"), defaultRetryMaxAttempts)
	if err != nil {
		return Config{}, err
	}
	cfg.RetryMaxAttempts = attempts

	backoffBase, err := parseDuration("RETRY_BACKOFF_BASE", getenv("RETRY_BACKOFF_BASE"), defaultRetryBackoffBase)
	if err != nil {
		return Config{}, err
	}
	cfg.RetryBackoffBase = backoffBase

	backoffMax, err := parseDuration("RETRY_BACKOFF_MAX", getenv("RETRY_BACKOFF_MAX"), defaultRetryBackoffMax)
	if err != nil {
		return Config{}, err
	}
	cfg.RetryBackoffMax = backoffMax

	healthTimeout, err := parseDuration("HEALTH_CHECK_TIMEOUT", getenv("HEALTH_CHECK_TIMEOUT"), defaultHealthCheckTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.HealthCheckTimeout = healthTimeout

	auditToken, err := requiredToken(getenv, "AUDIT_LOG_API_TOKEN")
	if err != nil {
		return Config{}, err
	}
	cfg.AuditLogAPIToken = auditToken

	persistenceToken, err := requiredToken(getenv, "PERSISTENCE_API_TOKEN")
	if err != nil {
		return Config{}, err
	}
	cfg.PersistenceAPIToken = persistenceToken

	return cfg, nil
}

func requiredToken(getenv func(string) string, key string) (string, error) {
	token := strings.TrimSpace(getenv(key))
	if token == "" {
		return "", fmt.Errorf("%s must be set and non-empty", key)
	}
	return token, nil
}

func envOrDefault(getenv func(string) string, key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}

// parsePositiveInt parsea un entero estrictamente positivo (p. ej. intentos de
// retry), donde el cero no es un valor válido: reintentar 0 veces no tiene
// sentido y es casi siempre un error de configuración.
func parsePositiveInt(name, raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", name, raw)
	}
	if value < 1 {
		return 0, fmt.Errorf("invalid %s %q: must be at least 1", name, raw)
	}
	return value, nil
}

func parsePageLimit(name, raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", name, raw)
	}
	if value < 0 {
		return 0, fmt.Errorf("invalid %s %q: must not be negative", name, raw)
	}
	return value, nil
}

func parseDuration(name, raw string, fallback time.Duration) (time.Duration, error) {
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, raw, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be positive", name, raw)
	}
	return parsed, nil
}

type byteSizeUnit struct {
	suffix string
	factor int64
}

var byteSizeUnits = []byteSizeUnit{
	{"GB", 1024 * 1024 * 1024},
	{"MB", 1024 * 1024},
	{"KB", 1024},
}

func parseByteSize(name, raw string, fallback int64) (int64, error) {
	if raw == "" {
		return fallback, nil
	}
	value, unit, err := splitByteSize(name, raw)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("invalid %s %q: must not be negative", name, raw)
	}
	if value > math.MaxInt64/unit {
		return 0, fmt.Errorf("invalid %s %q: value overflows int64", name, raw)
	}
	return value * unit, nil
}

func validatePort(name, raw string) error {
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid %s %q: must be a port between 1 and 65535", name, raw)
	}
	return nil
}

func validateBaseURL(name, raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid %s %q: must be an absolute http or https URL", name, raw)
	}
	return nil
}

func splitByteSize(name, raw string) (int64, int64, error) {
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return value, 1, nil
	}
	for _, unit := range byteSizeUnits {
		if strings.HasSuffix(strings.ToUpper(raw), unit.suffix) {
			number := strings.TrimSpace(raw[:len(raw)-len(unit.suffix)])
			value, err := strconv.ParseInt(number, 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("invalid %s %q", name, raw)
			}
			return value, unit.factor, nil
		}
	}
	return 0, 0, fmt.Errorf("invalid %s %q: unknown format", name, raw)
}
