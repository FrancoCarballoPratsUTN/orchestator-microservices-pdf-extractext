package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/handlers"
	"validationmicroservices-pdf-extractext/internal/httpapi"
)

const (
	pdfMediaType  = "application/pdf"
	jsonMediaType = "application/json"
)

func Routes(cfg config.Config, logger *slog.Logger, pdfHandler *handlers.PDFHandler, auditHandler *handlers.AuditHandler, textHandler *handlers.TextHandler) http.Handler {
	router := chi.NewRouter()
	router.Use(withRecovery(logger), withCORS(), withRequestID, withRequestLog(logger))

	router.Get("/healthz", statusEndpoint())
	router.Get("/readyz", statusEndpoint())

	router.Group(func(api chi.Router) {
		api.With(enforceContentType(pdfMediaType)).Post("/api/v1/pdfs/extract", pdfHandler.Extract)
		api.Get("/api/v1/audit/logs", auditHandler.List)
		api.With(enforceContentType(jsonMediaType)).Post("/api/v1/texts", textHandler.Create)
		api.Get("/api/v1/texts/{checksum}", textHandler.Find)
		api.With(enforceContentType(jsonMediaType)).Put("/api/v1/texts/{checksum}", textHandler.Update)
		api.Delete("/api/v1/texts/{checksum}", textHandler.Delete)
	})

	return router
}

func statusEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func withRequestLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", requestIDFromContext(r.Context()),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}