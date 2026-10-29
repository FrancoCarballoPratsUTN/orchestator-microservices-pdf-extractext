package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/handlers"
	"validationmicroservices-pdf-extractext/internal/httpapi"
	"validationmicroservices-pdf-extractext/internal/logging"
	"validationmicroservices-pdf-extractext/internal/reqctx"
)

const (
	pdfMediaType  = "application/pdf"
	jsonMediaType = "application/json"
)

func Routes(cfg config.Config, logger *slog.Logger, pdfHandler *handlers.PDFHandler, auditHandler *handlers.AuditHandler, textHandler *handlers.TextHandler, healthHandler *handlers.HealthHandler) http.Handler {
	router := chi.NewRouter()
	// El access log va antes del recovery a propósito: así un panic también
	// queda registrado como una petición más, con su 500. Si el recovery
	// envolviera al access log, el panic desenrollaría el access log y esa
	// petición no aparecería en él.
	router.Use(withRequestID, withRequestLog(logger), withRecovery(logger), withCORS())

	// API. Los hooks NotFound y MethodNotAllowed de chi devuelven texto plano o
	// el cuerpo vacío, así que se sobrescriben: todos los errores de la API, sin
	// excepción, deben ser problem documents RFC 9457. Un cliente que se encontrase
	// con un 404 de una URL mal escrita tenía que parsear dos formatos distintos
	// para el mismo tipo de error.
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteProblem(w, http.StatusNotFound, "Not Found", "no route matches "+r.Method+" "+r.URL.Path)
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed", r.Method+" is not supported on this path")
	})

	// /healthz es liveness estático (si dependiera de los MS, un MS caído
	// marcaría el contenedor unhealthy y Traefik lo sacaría del LB: no habría
	// degradación). /readyz agrega la salud de los MS y degrada a 503.
	router.Get("/healthz", statusEndpoint())
	router.Get("/readyz", healthHandler.Ready)

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

// withRequestLog escribe una línea por petición. Los campos son los de una
// lista blanca: método, path, status y duración. Nunca
// headers, nunca el cuerpo.
//
// La ruta y el método vienen del cliente, así que se acotan: sin un tope, un
// path largo convertiría la línea de log en un archivo.
func withRequestLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isHealthRequest(r) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(recorder, r)

			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			reqctx.Logger(r.Context(), logger).Log(r.Context(), accessLogLevel(status), "http request",
				"method", logging.Truncate(r.Method, maxMethodLength),
				"path", logging.Truncate(r.URL.Path, logging.MaxValueLength),
				"status", status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// statusRecorder envuelve el ResponseWriter para capturar el status y la
// cantidad de bytes escritos, que el access log necesita.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Flush reenvía al writer subyacente si lo soporta, para no romper streaming.
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func accessLogLevel(status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// isHealthRequest son las sondas que no pasan por el access log: se consultan
// con alta frecuencia y solo generarían ruido.
//
// El método importa: un POST a /healthz no es una sonda, es una petición
// equivocada que sí debe quedar registrada.
func isHealthRequest(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return r.URL.Path == "/healthz" || r.URL.Path == "/readyz"
}
