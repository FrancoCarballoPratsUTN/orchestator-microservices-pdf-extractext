package handlers

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/services"
)

const jsonMediaType = "application/json"

const maxTextBodyBytes = int64(1 << 20)

type TextHandler struct {
	service services.TextService
}

func NewTextHandler(service services.TextService) *TextHandler {
	return &TextHandler{service: service}
}

func (h *TextHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !isJSONRequest(r.Header.Get("Content-Type")) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Unsupported Media Type", "expected Content-Type "+jsonMediaType)
		return
	}

	body, err := readLimitedBody(w, r, maxTextBodyBytes)
	if err != nil {
		if errors.Is(err, errPayloadTooLarge) {
			writeProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "request body exceeds the maximum allowed size")
			return
		}
		writeProblem(w, http.StatusBadRequest, "Bad Request", "could not read request body")
		return
	}

	var request dto.CreateTextRequest
	if err := json.Unmarshal(body, &request); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "request body is not a valid CreateText JSON")
		return
	}

	document, err := h.service.Create(r.Context(), request)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, document)
}

func (h *TextHandler) writeServiceError(w http.ResponseWriter, err error) {
	var problem httpclient.Problem
	switch {
	case errors.Is(err, services.ErrEmptyChecksum):
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
	case errors.As(err, &problem):
		writeProblem(w, problem.Status, problem.Title, problem.Detail)
	default:
		writeProblem(w, http.StatusBadGateway, "Bad Gateway", "persistence service could not process the text")
	}
}

func isJSONRequest(rawContentType string) bool {
	mediaType, _, err := mime.ParseMediaType(rawContentType)
	return err == nil && mediaType == jsonMediaType
}