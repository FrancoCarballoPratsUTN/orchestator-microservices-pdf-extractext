package server

import (
	"log/slog"
	"net/http"
	"time"

	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/handlers"
)

func New(cfg config.Config, logger *slog.Logger, pdfHandler *handlers.PDFHandler, auditHandler *handlers.AuditHandler, textHandler *handlers.TextHandler, healthHandler *handlers.HealthHandler) *http.Server {
	return &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           Routes(cfg, logger, pdfHandler, auditHandler, textHandler, healthHandler),
		ReadHeaderTimeout: 5 * time.Second,
	}
}
