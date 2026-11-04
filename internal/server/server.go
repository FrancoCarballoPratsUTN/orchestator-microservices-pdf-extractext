package server

import (
	"log/slog"
	"net/http"
	"time"

	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/handlers"
)

const requestBudgetSlack = 10 * time.Second

// requestTimeout computes the server Read/Write timeout allowing for two
// serial upstream calls (e.g. Persistence check + Extract) with an extra slack.
func requestTimeout(httpTimeout time.Duration) time.Duration {
	return 2*httpTimeout + requestBudgetSlack
}

func New(cfg config.Config, logger *slog.Logger, pdfHandler *handlers.PDFHandler, auditHandler *handlers.AuditHandler, textHandler *handlers.TextHandler, healthHandler *handlers.HealthHandler) *http.Server {
	// PDF extraction may first perform a fail-open Persistence lookup and then
	// call Extract, each with its own HTTPTimeout budget.
	timeout := requestTimeout(cfg.HTTPTimeout)
	return &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           Routes(cfg, logger, pdfHandler, auditHandler, textHandler, healthHandler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       timeout,
		WriteTimeout:      timeout,
		IdleTimeout:       60 * time.Second,
	}
}
