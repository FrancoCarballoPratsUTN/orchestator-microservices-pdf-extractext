package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	extractClient := extract.NewClient(cfg.ExtractBaseURL, cfg.HTTPTimeout)
	auditLogClient := auditlog.NewClient(cfg.AuditLogBaseURL, cfg.HTTPTimeout)
	auditService := services.NewAuditService(auditLogClient, logger, cfg.HTTPTimeout)
	pdfService := services.NewPDFService(extractClient, auditService)
	textService := services.NewTextService(persistence.NewClient(cfg.PersistenceBaseURL, cfg.HTTPTimeout), auditService)
	auditHandler := handlers.NewAuditHandler(auditService)
	pdfHandler := handlers.NewPDFHandler(pdfService, cfg.MaxPDFSizeBytes)
	textHandler := handlers.NewTextHandler(textService, cfg.MaxTextBodyBytes)

	srv := server.New(cfg, logger, pdfHandler, auditHandler, textHandler)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-stopCh:
		logger.Info("shutting down", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
