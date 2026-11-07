package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"validationmicroservices-pdf-extractext/internal/logging"
)

// Config agrupa la configuración efectiva del orquestador.
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
	ExtractHealthURL     string
	PersistenceHealthURL string
	AuditLogHealthURL    string
	HealthCheckTimeout   time.Duration
	LogMaxValueLength    int
	LogMaxStackLength    int
	Consul               ConsulSettings
}

// Load construye la configuración desde las variables de entorno.
func Load() (Config, error) {
	return fromEnv(os.Getenv)
}

// LogLimits centraliza la política de acotado de logs. Es la única conversión
// de Config a logging.Limits: el composition root, las rutas y el log de
// arranque consumen este método para no repetir los campos a mano.
func (c Config) LogLimits() logging.Limits {
	return logging.Limits{
		MaxValueLength: c.LogMaxValueLength,
		MaxStackLength: c.LogMaxStackLength,
	}
}

// LoadFromValues construye la configuración a partir de valores ya cargados
// (por ejemplo, desde Consul KV), usando las variables de entorno como fallback.
func LoadFromValues(values map[string]string) (Config, error) {
	return fromEnv(func(key string) string {
		if value, ok := values[key]; ok {
			return value
		}
		return os.Getenv(key)
	})
}

func fromEnv(getenv func(string) string) (Config, error) {
	var cfg Config
	var err error
	for _, setting := range []struct {
		key    string
		target *string
	}{
		{key: "PORT", target: &cfg.Port},
		{key: "EXTRACT_BASE_URL", target: &cfg.ExtractBaseURL},
		{key: "PERSISTENCE_BASE_URL", target: &cfg.PersistenceBaseURL},
		{key: "AUDIT_LOG_BASE_URL", target: &cfg.AuditLogBaseURL},
		{key: "EXTRACT_HEALTH_URL", target: &cfg.ExtractHealthURL},
		{key: "PERSISTENCE_HEALTH_URL", target: &cfg.PersistenceHealthURL},
		{key: "AUDIT_LOG_HEALTH_URL", target: &cfg.AuditLogHealthURL},
	} {
		*setting.target, err = requiredSetting(getenv, setting.key)
		if err != nil {
			return Config{}, err
		}
	}
	if err := validatePort("PORT", cfg.Port); err != nil {
		return Config{}, err
	}
	for _, target := range []struct {
		name  string
		value string
	}{
		{"EXTRACT_BASE_URL", cfg.ExtractBaseURL},
		{"PERSISTENCE_BASE_URL", cfg.PersistenceBaseURL},
		{"AUDIT_LOG_BASE_URL", cfg.AuditLogBaseURL},
		{"EXTRACT_HEALTH_URL", cfg.ExtractHealthURL},
		{"PERSISTENCE_HEALTH_URL", cfg.PersistenceHealthURL},
		{"AUDIT_LOG_HEALTH_URL", cfg.AuditLogHealthURL},
	} {
		if err := validateBaseURL(target.name, target.value); err != nil {
			return Config{}, err
		}
	}

	timeout, err := parseDuration("HTTP_TIMEOUT", getenv("HTTP_TIMEOUT"))
	if err != nil {
		return Config{}, err
	}
	cfg.HTTPTimeout = timeout

	shutdownTimeout, err := parseDuration("SHUTDOWN_TIMEOUT", getenv("SHUTDOWN_TIMEOUT"))
	if err != nil {
		return Config{}, err
	}
	cfg.ShutdownTimeout = shutdownTimeout

	size, err := parseByteSize("MAX_PDF_SIZE_BYTES", getenv("MAX_PDF_SIZE_BYTES"))
	if err != nil {
		return Config{}, err
	}
	cfg.MaxPDFSizeBytes = size

	textSize, err := parseByteSize("MAX_TEXT_BODY_BYTES", getenv("MAX_TEXT_BODY_BYTES"))
	if err != nil {
		return Config{}, err
	}
	cfg.MaxTextBodyBytes = textSize

	pages, err := parsePageLimit("MAX_PDF_PAGES", getenv("MAX_PDF_PAGES"))
	if err != nil {
		return Config{}, err
	}
	cfg.MaxPDFPages = pages

	healthTimeout, err := parseDuration("HEALTH_CHECK_TIMEOUT", getenv("HEALTH_CHECK_TIMEOUT"))
	if err != nil {
		return Config{}, err
	}
	cfg.HealthCheckTimeout = healthTimeout

	logMaxValueLength, err := parsePositiveInt("LOG_MAX_VALUE_LENGTH", strings.TrimSpace(getenv("LOG_MAX_VALUE_LENGTH")))
	if err != nil {
		return Config{}, err
	}
	cfg.LogMaxValueLength = logMaxValueLength

	logMaxStackLength := strings.TrimSpace(getenv("LOG_MAX_STACK_LENGTH"))
	if logMaxStackLength == "" {
		cfg.LogMaxStackLength = logging.DefaultMaxStackLength
	} else {
		parsed, parseErr := parsePositiveInt("LOG_MAX_STACK_LENGTH", logMaxStackLength)
		if parseErr != nil {
			return Config{}, parseErr
		}
		cfg.LogMaxStackLength = parsed
	}

	auditToken, err := requiredSetting(getenv, "AUDIT_LOG_API_TOKEN")
	if err != nil {
		return Config{}, err
	}
	cfg.AuditLogAPIToken = auditToken

	persistenceToken, err := requiredSetting(getenv, "PERSISTENCE_API_TOKEN")
	if err != nil {
		return Config{}, err
	}
	cfg.PersistenceAPIToken = persistenceToken

	consulSettings, err := LoadConsulSettings(getenv)
	if err != nil {
		return Config{}, err
	}
	cfg.Consul = consulSettings

	return cfg, nil
}

func requiredSetting(getenv func(string) string, key string) (string, error) {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return "", fmt.Errorf("%s must be set and non-empty", key)
	}
	return value, nil
}

func parsePositiveInt(name, raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("%s must be set and greater than zero", name)
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

func parsePageLimit(name, raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("%s must be set", name)
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

func parseDuration(name, raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("%s must be set and positive", name)
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

func parseByteSize(name, raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("%s must be set", name)
	}
	value, unit, err := splitByteSize(name, raw)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be positive", name, raw)
	}
	if value > math.MaxInt64/unit {
		return 0, fmt.Errorf("invalid %s %q: value overflows int64", name, raw)
	}
	return value * unit, nil
}

func validatePort(name, raw string) error {
	raw = strings.TrimSpace(raw)
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid %s %q: must be a port between 1 and 65535", name, raw)
	}
	return nil
}

func validateBaseURL(name, raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed == nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid %s: must be an absolute http or https URL", name)
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
