package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"validationmicroservices-pdf-extractext/internal/clients/auditlog"
	"validationmicroservices-pdf-extractext/internal/clients/extract"
	"validationmicroservices-pdf-extractext/internal/clients/persistence"
	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/handlers"
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
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stopCh)

	return runWithSignals(context.Background(), logger, cfg, stopCh)
}

// runWithSignals arma el orquestador y lo apaga al recibir una señal en stopCh.
// El canal se inyecta para poder testear el path de shutdown sin señales del SO.
func runWithSignals(ctx context.Context, logger *slog.Logger, cfg config.Config, stopCh <-chan os.Signal) error {
	logStartup(logger, cfg)

	extractClient := extract.NewClientWithLogger(cfg.ExtractBaseURL, cfg.HTTPTimeout, logger)
	auditLogClient := auditlog.NewClientWithLogger(cfg.AuditLogBaseURL, cfg.HTTPTimeout, cfg.AuditLogAPIToken, logger)
	persistenceClient := persistence.NewClientWithLogger(cfg.PersistenceBaseURL, cfg.HTTPTimeout, cfg.PersistenceAPIToken, logger)
	auditService := services.NewAuditService(auditLogClient, logger, cfg.HTTPTimeout)
	pdfService := services.NewPDFService(extractClient, persistenceClient, auditService, logger, cfg.MaxPDFPages)
	textService := services.NewTextService(persistenceClient, auditService)
	auditHandler := handlers.NewAuditHandler(auditService, logger)
	pdfHandler := handlers.NewPDFHandler(pdfService, cfg.MaxPDFSizeBytes, logger)
	textHandler := handlers.NewTextHandler(textService, cfg.MaxTextBodyBytes, logger)

	srv := server.New(cfg, logger, pdfHandler, auditHandler, textHandler)

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

// logStartup registra la configuración efectiva del orquestador al arrancar.
// Nunca incluye tokens ni otros secretos: solo URLs, timeouts y límites.
func logStartup(logger *slog.Logger, cfg config.Config) {
	logger.Info("application starting",
		"addr", ":"+cfg.Port,
		"extract_base_url", cfg.ExtractBaseURL,
		"persistence_base_url", cfg.PersistenceBaseURL,
		"audit_log_base_url", cfg.AuditLogBaseURL,
		"http_timeout", cfg.HTTPTimeout.String(),
		"shutdown_timeout", cfg.ShutdownTimeout.String(),
		"max_pdf_size_bytes", cfg.MaxPDFSizeBytes,
		"max_text_body_bytes", cfg.MaxTextBodyBytes,
		"max_pdf_pages", cfg.MaxPDFPages,
	)
}
