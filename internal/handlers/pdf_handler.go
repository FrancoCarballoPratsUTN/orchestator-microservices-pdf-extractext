package handlers

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"validationmicroservices-pdf-extractext/internal/httpapi"
	"validationmicroservices-pdf-extractext/internal/services"
	"validationmicroservices-pdf-extractext/internal/validation"
)

var errPayloadTooLarge = errors.New("pdf exceeds size limit")

type PDFHandler struct {
	service services.PDFService
	maxSize int64
	logger  *slog.Logger
}

func NewPDFHandler(service services.PDFService, maxPDFSize int64, logger *slog.Logger) *PDFHandler {
	return &PDFHandler{service: service, maxSize: maxPDFSize, logger: logger}
}

func (h *PDFHandler) Extract(w http.ResponseWriter, r *http.Request) {
	pdfData, err := readLimitedBody(w, r, h.maxSize)
	if err != nil {
		if errors.Is(err, errPayloadTooLarge) {
			logDecision(r.Context(), h.logger, "pdf rejected: body exceeds size limit", http.StatusRequestEntityTooLarge)
			httpapi.WriteProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "request body exceeds the maximum allowed PDF size")
			return
		}
		logDecision(r.Context(), h.logger, "pdf rejected: could not read request body", http.StatusBadRequest, "error", err.Error())
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

// writeServiceError maps validation failures to their HTTP status through the
// validation package, which owns that mapping. Anything else is an Extract
// failure, flattened to 502.
func (h *PDFHandler) writeServiceError(r *http.Request, w http.ResponseWriter, err error) {
	filename := r.Header.Get("X-Filename")
	if status, title, ok := validation.StatusOf(err); ok {
		logDecision(r.Context(), h.logger, "pdf rejected by validation", status, "error", err.Error(), "filename", filename)
		httpapi.WriteProblem(w, status, title, err.Error())
		return
	}
	logDecision(r.Context(), h.logger, "extract service failed", http.StatusBadGateway, "error", err.Error(), "filename", filename)
	httpapi.WriteProblem(w, http.StatusBadGateway, "Bad Gateway", "extract service could not process the PDF")
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
