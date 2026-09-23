package server

import (
	"log/slog"
	"net/http"
	"time"

	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/handlers"
)

func New(cfg config.Config, logger *slog.Logger, pdfHandler *handlers.PDFHandler) *http.Server {
	return &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           Routes(cfg, logger, pdfHandler),
		ReadHeaderTimeout: 5 * time.Second,
	}
}
