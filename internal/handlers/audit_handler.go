package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpapi"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/services"
)

const (
	defaultSkip  = 0
	defaultLimit = 10
)

type AuditHandler struct {
	service services.AuditService
}

func NewAuditHandler(service services.AuditService) *AuditHandler {
	return &AuditHandler{service: service}
}

func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	params, ok := auditQueryParams(r.URL.Query())
	if !ok {
		httpapi.WriteProblem(w, http.StatusBadRequest, "Bad Request", "query parameters skip and limit must be integers")
		return
	}

	response, err := h.service.FetchLogs(r.Context(), params)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, response)
}

func auditQueryParams(query url.Values) (dto.AuditQueryParams, bool) {
	skip, ok := intQueryParam(query, "skip", defaultSkip)
	if !ok {
		return dto.AuditQueryParams{}, false
	}
	limit, ok := intQueryParam(query, "limit", defaultLimit)
	if !ok {
		return dto.AuditQueryParams{}, false
	}
	return dto.AuditQueryParams{
		Checksum: models.Checksum(query.Get("checksum")),
		Skip:     skip,
		Limit:    limit,
	}, true
}

func intQueryParam(query url.Values, key string, fallback int) (int, bool) {
	raw := query.Get(key)
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return value, true
}

func (h *AuditHandler) writeServiceError(w http.ResponseWriter, err error) {
	var problem httpclient.Problem
	if errors.As(err, &problem) {
		httpapi.WriteProblem(w, problem.Status, problem.Title, problem.Detail)
		return
	}
	httpapi.WriteProblem(w, http.StatusBadGateway, "Bad Gateway", "audit log service is unreachable")
}