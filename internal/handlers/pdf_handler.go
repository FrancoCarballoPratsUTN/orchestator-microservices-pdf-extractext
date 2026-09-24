package handlers

import (
	"errors"
	"io"
	"net/http"

	"validationmicroservices-pdf-extractext/internal/httpapi"
	"validationmicroservices-pdf-extractext/internal/services"
)

var errPayloadTooLarge = errors.New("pdf exceeds size limit")

type PDFHandler struct {
	service services.PDFService
	maxSize int64
}

func NewPDFHandler(service services.PDFService, maxPDFSize int64) *PDFHandler {
	return &PDFHandler{service: service, maxSize: maxPDFSize}
}

func (h *PDFHandler) Extract(w http.ResponseWriter, r *http.Request) {
	pdfData, err := readLimitedBody(w, r, h.maxSize)
	if err != nil {
		if errors.Is(err, errPayloadTooLarge) {
			httpapi.WriteProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "request body exceeds the maximum allowed PDF size")
			return
		}
		httpapi.WriteProblem(w, http.StatusBadRequest, "Bad Request", "could not read request body")
		return
	}

	document, err := h.service.IngestAndExtract(r.Context(), pdfData)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, document)
}

func (h *PDFHandler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidPDF):
		httpapi.WriteProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
	default:
		httpapi.WriteProblem(w, http.StatusBadGateway, "Bad Gateway", "extract service could not process the PDF")
	}
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