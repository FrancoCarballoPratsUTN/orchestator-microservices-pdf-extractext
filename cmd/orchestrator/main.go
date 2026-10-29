package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"validationmicroservices-pdf-extractext/internal/clients/auditlog"
	"validationmicroservices-pdf-extractext/internal/clients/extract"
	"validationmicroservices-pdf-extractext/internal/clients/persistence"
	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/consul"
	"validationmicroservices-pdf-extractext/internal/handlers"
	"validationmicroservices-pdf-extractext/internal/health"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/logging"
	"validationmicroservices-pdf-extractext/internal/server"
	"validationmicroservices-pdf-extractext/internal/services"
)

func main() {
	logger, err := logging.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid logging configuration: %v\n", err)
		os.Exit(1)
	}
	if err := run(logger); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stopCh)

	return runWithSignals(context.Background(), logger, cfg, stopCh)
}

func loadConfig() (config.Config, error) {
	if !strings.EqualFold(os.Getenv("CONSUL_ENABLED"), "true") {
		return config.Load()
	}

	baseURL := strings.TrimSpace(os.Getenv("CONSUL_HTTP_ADDR"))
	token := strings.TrimSpace(os.Getenv("CONSUL_HTTP_TOKEN"))
	prefix := strings.TrimSpace(os.Getenv("CONSUL_KV_PREFIX"))
	if baseURL == "" || token == "" || prefix == "" {
		return config.Config{}, fmt.Errorf("CONSUL_HTTP_ADDR, CONSUL_HTTP_TOKEN and CONSUL_KV_PREFIX are required when CONSUL_ENABLED=true")
	}

	client, err := consul.NewKVClientWithCA(baseURL, token, os.Getenv("CONSUL_HTTP_CA_FILE"))
	if err != nil {
		return config.Config{}, err
	}
	values, err := client.LoadPrefix(prefix)
	if err != nil {
		return config.Config{}, err
	}
	return config.LoadFromValues(values)
}

// runWithSignals arma el orquestador y lo apaga al recibir una señal en stopCh.
// El canal se inyecta para poder testear el path de shutdown sin señales del SO.
func runWithSignals(ctx context.Context, logger *slog.Logger, cfg config.Config, stopCh <-chan os.Signal) error {
	logStartup(logger, cfg)

	// Extract: 3 intentos por default (permite POST). El retry de métodos no
	// idempotentes lo habilita el propio cliente.
	extractPolicy := httpclient.RetryPolicy{
		Attempts:    cfg.RetryMaxAttempts,
		BackoffBase: cfg.RetryBackoffBase,
		BackoffMax:  cfg.RetryBackoffMax,
	}
	extractClient := extract.NewClientWithPolicy(cfg.ExtractBaseURL, cfg.HTTPTimeout, extractPolicy, logger)
	auditLogClient := auditlog.NewClientWithLogger(cfg.AuditLogBaseURL, cfg.HTTPTimeout, cfg.AuditLogAPIToken, logger)
	persistenceClient := persistence.NewClientWithLogger(cfg.PersistenceBaseURL, cfg.HTTPTimeout, cfg.PersistenceAPIToken, logger)
	auditService := services.NewAuditService(auditLogClient, logger, cfg.HTTPTimeout)
	pdfService := services.NewPDFService(extractClient, persistenceClient, auditService, logger, cfg.MaxPDFPages)
	textService := services.NewTextService(persistenceClient, auditService)
	auditHandler := handlers.NewAuditHandler(auditService, logger)
	pdfHandler := handlers.NewPDFHandler(pdfService, cfg.MaxPDFSizeBytes, logger)
	textHandler := handlers.NewTextHandler(textService, cfg.MaxTextBodyBytes, logger)

	// /readyz sondea a los MS por DNS directo en la red backend (independiente
	// del circuit breaker y del LB de Traefik): sólo sondeos de salud, no
	// tráfico de negocio.
	healthChecker := health.NewChecker([]health.Probe{
		{Name: health.ServiceExtract, URL: cfg.ExtractHealthURL},
		{Name: health.ServicePersistence, URL: cfg.PersistenceHealthURL},
		{Name: health.ServiceAudit, URL: cfg.AuditLogHealthURL},
	}, cfg.HealthCheckTimeout, logger)
	healthHandler := handlers.NewHealthHandler(healthChecker, logger)

	srv := server.New(cfg, logger, pdfHandler, auditHandler, textHandler, healthHandler)
	consulClient, registration, err := serviceRegistration(cfg)
	if err != nil {
		return err
	}
	deregister := func() {}
	if consulClient != nil {
		if err := consulClient.RegisterService(registration); err != nil {
			return err
		}
		deregistered := false
		deregister = func() {
			if deregistered {
				return
			}
			if err := consulClient.DeregisterService(registration.ID); err != nil {
				logger.Warn("Consul service deregistration failed", "error", err)
				return
			}
			deregistered = true
			logger.Info("deregistered from Consul", "service_id", registration.ID)
		}
		// Fallback para las salidas que no pasan por el apagado normal.
		defer deregister()
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case sig := <-stopCh:
		logger.Info("shutting down", "signal", sig.String())
	}

	// Dejar de anunciarse antes de drenar: una instancia que va a cerrarse no
	// debe seguir recibiendo tráfico nuevo. El defer de arriba queda como
	// fallback y no repite la llamada si ésta tuvo éxito.
	deregister()

	// Un único presupuesto (SHUTDOWN_TIMEOUT) rige ambos pasos: dejar de aceptar
	// conexiones y drenar la auditoría async. Si el primero lo agota, el segundo
	// no tiene budget y el proceso igual termina: nunca se cuelga más allá del
	// deadline configurado.
	shutdownCtx, cancel := context.WithTimeout(ctx, cfg.ShutdownTimeout)
	defer cancel()

	var shutdownErr error
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http shutdown did not complete", "error", err)
		shutdownErr = err
	}

	logger.Info("draining audit emissions")
	if err := auditService.Close(shutdownCtx); err != nil {
		logger.Warn("audit drain did not complete", "error", err)
		if shutdownErr == nil {
			shutdownErr = err
		}
	} else {
		logger.Info("audit emissions drained")
	}

	return shutdownErr
}

func serviceRegistration(cfg config.Config) (*consul.KVClient, consul.ServiceRegistration, error) {
	if !strings.EqualFold(os.Getenv("CONSUL_ENABLED"), "true") {
		return nil, consul.ServiceRegistration{}, nil
	}
	port, err := strconv.Atoi(cfg.Port)
	if err != nil {
		return nil, consul.ServiceRegistration{}, fmt.Errorf("invalid service port %q: %w", cfg.Port, err)
	}
	address := strings.TrimSpace(os.Getenv("CONSUL_SERVICE_ADDRESS"))
	if address == "" {
		address, err = os.Hostname()
		if err != nil {
			return nil, consul.ServiceRegistration{}, fmt.Errorf("resolve service hostname: %w", err)
		}
	}
	name := os.Getenv("CONSUL_SERVICE_NAME")
	if name == "" {
		name = "orchestrator"
	}
	id := strings.TrimSpace(os.Getenv("CONSUL_SERVICE_ID"))
	if id == "" {
		id = name + "-" + address
	}
	check := os.Getenv("CONSUL_SERVICE_CHECK_URL")
	if check == "" {
		check = "http://" + address + ":" + cfg.Port + "/readyz"
	}
	tags := nonEmptyTags(os.Getenv("CONSUL_SERVICE_TAGS"))
	client, err := consul.NewKVClientWithCA(
		os.Getenv("CONSUL_HTTP_ADDR"),
		os.Getenv("CONSUL_HTTP_TOKEN"),
		os.Getenv("CONSUL_HTTP_CA_FILE"),
	)
	if err != nil {
		return nil, consul.ServiceRegistration{}, err
	}
	return client, consul.ServiceRegistration{
		ID: id, Name: name, Address: address, Port: port, Check: check, Tags: tags,
	}, nil
}

func nonEmptyTags(raw string) []string {
	var tags []string
	// Separador de tags: ';'. Las comas son parte del valor (p. ej. la lista de
	// middlewares de Traefik: "security-headers@file,api-ratelimit@file"), por lo
	// que NO deben usarse como separador.
	for _, tag := range strings.Split(raw, ";") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// logStartup registra la configuración efectiva del orquestador al arrancar.
// Nunca incluye tokens ni otros secretos: solo URLs, timeouts y límites.
func logStartup(logger *slog.Logger, cfg config.Config) {
	logger.Info("application starting",
		"addr", ":"+cfg.Port,
		"extract_base_url", logging.SafeURL(cfg.ExtractBaseURL),
		"persistence_base_url", logging.SafeURL(cfg.PersistenceBaseURL),
		"audit_log_base_url", logging.SafeURL(cfg.AuditLogBaseURL),
		"http_timeout", cfg.HTTPTimeout.String(),
		"shutdown_timeout", cfg.ShutdownTimeout.String(),
		"max_pdf_size_bytes", cfg.MaxPDFSizeBytes,
		"max_text_body_bytes", cfg.MaxTextBodyBytes,
		"max_pdf_pages", cfg.MaxPDFPages,
	)
}
