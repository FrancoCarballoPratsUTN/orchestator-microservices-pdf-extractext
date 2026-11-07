package handlers

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"validationmicroservices-pdf-extractext/internal/httpapi"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/logging"
	"validationmicroservices-pdf-extractext/internal/services"
	"validationmicroservices-pdf-extractext/internal/validation"
)

var errPayloadTooLarge = errors.New("pdf exceeds size limit")

// PDFHandler atiende el endpoint de extracción de PDFs.
type PDFHandler struct {
	service   services.PDFService
	maxSize   int64
	logger    *slog.Logger
	logLimits logging.Limits
}

// PDFHandlerOptions agrupa las dependencias y límites de PDFHandler.
type PDFHandlerOptions struct {
	Service services.PDFService
	MaxSize int64
	Logger  *slog.Logger
	Limits  logging.Limits
}

// NewPDFHandler construye un PDFHandler con las opciones provistas.
func NewPDFHandler(opts PDFHandlerOptions) *PDFHandler {
	return &PDFHandler{
		service:   opts.Service,
		maxSize:   opts.MaxSize,
		logger:    opts.Logger,
		logLimits: opts.Limits,
	}
}

// Extract lee el cuerpo de la petición y delega la extracción en el servicio.
func (h *PDFHandler) Extract(w http.ResponseWriter, r *http.Request) {
	pdfData, err := readLimitedBody(w, r, h.maxSize)
	if err != nil {
		if errors.Is(err, errPayloadTooLarge) {
			logDecision(r.Context(), h.logger, "pdf rejected: body exceeds size limit", http.StatusRequestEntityTooLarge)
			httpapi.WriteProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "request body exceeds the maximum allowed PDF size")
			return
		}
		logDecision(r.Context(), h.logger, "pdf rejected: could not read request body", http.StatusBadRequest, "error", errorDetail(err, h.logLimits))
		httpapi.WriteProblem(w, http.StatusBadRequest, "Bad Request", "could not read request body")
		return
	}

	document, err := h.service.IngestAndExtract(r.Context(), pdfData, r.Header.Get("X-Filename"))
	if err != nil {
		h.writeServiceError(r, w, err)
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, document)
}

// writeServiceError mapea los fallos de validación a su status HTTP mediante el
// paquete validation, que es dueño de ese mapeo. Cualquier otro error es un
// fallo del Extract y se aplana a 502.
func (h *PDFHandler) writeServiceError(r *http.Request, w http.ResponseWriter, err error) {
	filename := logging.Truncate(r.Header.Get("X-Filename"), h.logLimits.ValueLength())
	detail := errorDetail(err, h.logLimits)
	if status, title, ok := validation.StatusOf(err); ok {
		logDecision(r.Context(), h.logger, "pdf rejected by validation", status, "error", detail, "filename", filename)
		httpapi.WriteProblem(w, status, title, "request rejected by PDF validation")
		return
	}
	if isServiceUnavailable(err) {
		logDecision(r.Context(), h.logger, "extract service is unavailable", http.StatusServiceUnavailable, "error", detail, "filename", filename)
		httpapi.WriteProblem(w, http.StatusServiceUnavailable, "Service Unavailable", "extract service is temporarily unavailable")
		return
	}
	logDecision(r.Context(), h.logger, "extract service failed", http.StatusBadGateway, "error", detail, "filename", filename)
	httpapi.WriteProblem(w, http.StatusBadGateway, "Bad Gateway", "extract service could not process the PDF")
}

func isServiceUnavailable(err error) bool {
	code, ok := httpclient.StatusCode(err)
	return ok && code == http.StatusServiceUnavailable
}

func readLimitedBody(w http.ResponseWriter, r *http.Request, maxSize int64) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return nil, errPayloadTooLarge
		}
		return nil, err
	}
	return data, nil
}
