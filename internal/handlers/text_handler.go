package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpapi"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/services"
)

type TextHandler struct {
	service          services.TextService
	maxTextBodyBytes int64
}

func NewTextHandler(service services.TextService, maxTextBodyBytes int64) *TextHandler {
	return &TextHandler{service: service, maxTextBodyBytes: maxTextBodyBytes}
}

func (h *TextHandler) Create(w http.ResponseWriter, r *http.Request) {
	body, err := readLimitedBody(w, r, h.maxTextBodyBytes)
	if err != nil {
		if errors.Is(err, errPayloadTooLarge) {
			httpapi.WriteProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "request body exceeds the maximum allowed size")
			return
		}
		httpapi.WriteProblem(w, http.StatusBadRequest, "Bad Request", "could not read request body")
		return
	}

	var request dto.CreateTextRequest
	if err := json.Unmarshal(body, &request); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "Bad Request", "request body is not a valid CreateText JSON")
		return
	}

	document, err := h.service.Create(r.Context(), request)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpapi.WriteJSON(w, http.StatusCreated, document)
}

func (h *TextHandler) Find(w http.ResponseWriter, r *http.Request) {
	text, err := h.service.FindByChecksum(r.Context(), pathChecksum(r))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, text)
}

func (h *TextHandler) Update(w http.ResponseWriter, r *http.Request) {
	body, err := readLimitedBody(w, r, h.maxTextBodyBytes)
	if err != nil {
		if errors.Is(err, errPayloadTooLarge) {
			httpapi.WriteProblem(w, http.StatusRequestEntityTooLarge, "Payload Too Large", "request body exceeds the maximum allowed size")
			return
		}
		httpapi.WriteProblem(w, http.StatusBadRequest, "Bad Request", "could not read request body")
		return
	}

	if hasImmutableField(body) {
		httpapi.WriteProblem(w, http.StatusBadRequest, "Bad Request", "text and checksum are immutable")
		return
	}

	var request dto.UpdateTextRequest
	if err := json.Unmarshal(body, &request); err != nil {
		httpapi.WriteProblem(w, http.StatusBadRequest, "Bad Request", "request body is not a valid UpdateText JSON")
		return
	}

	text, err := h.service.Update(r.Context(), pathChecksum(r), request)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, text)
}

func (h *TextHandler) Delete(w http.ResponseWriter, r *http.Request) {
	response, err := h.service.Delete(r.Context(), pathChecksum(r))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, response)
}

func hasImmutableField(body []byte) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return false
	}
	_, hasText := fields["text"]
	_, hasChecksum := fields["checksum"]
	return hasText || hasChecksum
}

func pathChecksum(r *http.Request) models.Checksum {
	raw := r.URL.Path
	if index := strings.LastIndexByte(raw, '/'); index >= 0 && index+1 < len(raw) {
		return models.Checksum(raw[index+1:])
	}
	return models.Checksum(raw)
}

func (h *TextHandler) writeServiceError(w http.ResponseWriter, err error) {
	var problem httpclient.Problem
	switch {
	case errors.Is(err, services.ErrEmptyChecksum):
		httpapi.WriteProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
	case errors.As(err, &problem):
		httpapi.WriteProblem(w, problem.Status, problem.Title, problem.Detail)
	default:
		httpapi.WriteProblem(w, http.StatusBadGateway, "Bad Gateway", "persistence service could not process the text")
	}
}