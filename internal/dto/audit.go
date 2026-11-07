package dto

import "validationmicroservices-pdf-extractext/internal/models"

// AuditQueryParams agrupa los filtros y la paginación de la consulta de auditoría.
type AuditQueryParams struct {
	Checksum models.Checksum
	Skip     int
	Limit    int
}

// AuditLogsResponse es la respuesta con los eventos de auditoría consultados.
type AuditLogsResponse struct {
	Logs []models.AuditLog `json:"logs"`
}
