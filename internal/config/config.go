package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPort               = "8080"
	defaultExtractBaseURL     = "http://localhost:8081"
	defaultPersistenceBaseURL = "http://localhost:8082"
	defaultAuditLogBaseURL    = "http://localhost:8083"
	defaultHTTPTimeout        = 10 * time.Second
	defaultMaxPDFSizeBytes    = int64(15 * 1024 * 1024)
	defaultMaxTextBodyBytes   = defaultMaxPDFSizeBytes
)

type Config struct {
	Port               string
	ExtractBaseURL     string
	PersistenceBaseURL string
	AuditLogBaseURL    string
	HTTPTimeout        time.Duration
	MaxPDFSizeBytes    int64
	MaxTextBodyBytes   int64
}

func Load() (Config, error) {
	return fromEnv(os.Getenv)
}

func fromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		Port:               envOrDefault(getenv, "PORT", defaultPort),
		ExtractBaseURL:     envOrDefault(getenv, "EXTRACT_BASE_URL", defaultExtractBaseURL),
		PersistenceBaseURL: envOrDefault(getenv, "PERSISTENCE_BASE_URL", defaultPersistenceBaseURL),
		AuditLogBaseURL:    envOrDefault(getenv, "AUDIT_LOG_BASE_URL", defaultAuditLogBaseURL),
		HTTPTimeout:        defaultHTTPTimeout,
		MaxPDFSizeBytes:    defaultMaxPDFSizeBytes,
		MaxTextBodyBytes:   defaultMaxTextBodyBytes,
	}

	timeout, err := parseDuration(getenv("HTTP_TIMEOUT"), defaultHTTPTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.HTTPTimeout = timeout

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

	return cfg, nil
}

func envOrDefault(getenv func(string) string, key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}

func parseDuration(raw string, fallback time.Duration) (time.Duration, error) {
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid HTTP_TIMEOUT %q: %w", raw, err)
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
	return value * unit, nil
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
