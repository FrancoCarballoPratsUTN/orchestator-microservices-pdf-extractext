package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"validationmicroservices-pdf-extractext/internal/health"
	"validationmicroservices-pdf-extractext/internal/httpapi"
)

// HealthChecker agrega la salud de las dependencias. Se define como interfaz
// para poder sustituirlo en los tests sin levantar los microservicios.
type HealthChecker interface {
	Check(ctx context.Context) health.Report
}

// HealthHandler expone la readiness agregada en /readyz. A diferencia de
// /healthz (liveness estático), consulta a los MS y degrada a 503 si alguno
// está caído, para que el orquestador deje de recibir tráfico en vez de fallar
// cada request.
type HealthHandler struct {
	checker HealthChecker
	logger  *slog.Logger
}

func NewHealthHandler(checker HealthChecker, logger *slog.Logger) *HealthHandler {
	return &HealthHandler{checker: checker, logger: logger}
}

func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	report := h.checker.Check(r.Context())

	status := http.StatusOK
	if report.Status != health.StatusOK {
		status = http.StatusServiceUnavailable
		logDecision(r.Context(), h.logger, "orchestrator degraded", status,
			"dependencies", len(report.Dependencies))
	}
	httpapi.WriteJSON(w, status, report)
}
