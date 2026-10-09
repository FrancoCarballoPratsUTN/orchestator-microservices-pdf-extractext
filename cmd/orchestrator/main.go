package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"validationmicroservices-pdf-extractext/internal/clients/auditlog"
	"validationmicroservices-pdf-extractext/internal/clients/extract"
	"validationmicroservices-pdf-extractext/internal/clients/persistence"
	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/handlers"
	"validationmicroservices-pdf-extractext/internal/server"
	"validationmicroservices-pdf-extractext/internal/services"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
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
	extractClient := extract.NewClient(cfg.ExtractBaseURL, cfg.HTTPTimeout)
	auditLogClient := auditlog.NewClient(cfg.AuditLogBaseURL, cfg.HTTPTimeout, cfg.AuditLogAPIToken)
	persistenceClient := persistence.NewClient(cfg.PersistenceBaseURL, cfg.HTTPTimeout, cfg.PersistenceAPIToken)
	auditService := services.NewAuditService(auditLogClient, logger, cfg.HTTPTimeout)
	pdfService := services.NewPDFService(extractClient, persistenceClient, auditService, logger, cfg.MaxPDFPages)
	textService := services.NewTextService(persistenceClient, auditService)
	auditHandler := handlers.NewAuditHandler(auditService)
	pdfHandler := handlers.NewPDFHandler(pdfService, cfg.MaxPDFSizeBytes)
	textHandler := handlers.NewTextHandler(textService, cfg.MaxTextBodyBytes)

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
