package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/handlers"
)

func Routes(cfg config.Config, logger *slog.Logger, pdfHandler *handlers.PDFHandler, auditHandler *handlers.AuditHandler, textHandler *handlers.TextHandler) http.Handler {
	router := chi.NewRouter()
	router.Use(withRequestLog(logger))
	router.Get("/healthz", healthz())
	router.Post("/api/v1/pdfs/extract", pdfHandler.Extract)
	router.Get("/api/v1/audit/logs", auditHandler.List)
	router.Post("/api/v1/texts", textHandler.Create)
	router.Get("/api/v1/texts/{checksum}", textHandler.Find)
	router.Put("/api/v1/texts/{checksum}", textHandler.Update)
	router.Delete("/api/v1/texts/{checksum}", textHandler.Delete)
	return router
}

func healthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func withRequestLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}
