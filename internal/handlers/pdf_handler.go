package handlers

import (
	"errors"
	"io"
	"mime"
	"net/http"

	"validationmicroservices-pdf-extractext/internal/services"
)

const pdfMediaType = "application/pdf"

var errPayloadTooLarge = errors.New("pdf exceeds size limit")

type PDFHandler struct {
	service services.PDFService
	maxSize int64
}

func NewPDFHandler(service services.PDFService, maxPDFSize int64) *PDFHandler {
	return &PDFHandler{service: service, maxSize: maxPDFSize}
}

func (h *PDFHandler) Extract(w http.ResponseWriter, r *http.Request) {
	if !isPDFRequest(r.Header.Get("Content-Type")) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", "expected Content-Type "+pdfMediaType)
		return
	}

	pdfData, err := readLimitedBody(w, r, h.maxSize)
	if err != nil {
		if errors.Is(err, errPayloadTooLarge) {
			writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "request body exceeds the maximum allowed PDF size")
			return
		}
		writeProblem(w, http.StatusBadRequest, "Bad Request", "could not read request body")
		return
	}

	document, err := h.service.IngestAndExtract(r.Context(), pdfData)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, document)
}

func (h *PDFHandler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidPDF):
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
	default:
		writeProblem(w, http.StatusBadGateway, "Bad Gateway", "extract service could not process the PDF")
	}
}

func isPDFRequest(rawContentType string) bool {
	mediaType, _, err := mime.ParseMediaType(rawContentType)
	return err == nil && mediaType == pdfMediaType
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