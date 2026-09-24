package dto

import "validationmicroservices-pdf-extractext/internal/models"

type AuditQueryParams struct {
	Checksum models.Checksum
	Skip     int
	Limit    int
}

type AuditLogsResponse struct {
	Logs []models.AuditLog `json:"logs"`
}