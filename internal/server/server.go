package server

import (
	"log/slog"
	"net/http"
	"time"

	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/handlers"
)

const requestBudgetSlack = 10 * time.Second

// requestTimeout calcula el timeout de lectura/escritura del servidor dejando
// espacio para dos llamadas seriales a servicios upstream (por ejemplo, la
// consulta a Persistence y la llamada a Extract) más un margen extra.
func requestTimeout(httpTimeout time.Duration) time.Duration {
	return 2*httpTimeout + requestBudgetSlack
}

// New construye el servidor HTTP con los timeouts derivados de HTTPTimeout.
func New(cfg config.Config, logger *slog.Logger, pdfHandler *handlers.PDFHandler, auditHandler *handlers.AuditHandler, textHandler *handlers.TextHandler, healthHandler *handlers.HealthHandler) *http.Server {
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
