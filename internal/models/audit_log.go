package models

import "time"

// OperationType identifica el tipo de operación auditada.
type OperationType string

// Operaciones auditables.
const (
	OpPDFExtract OperationType = "pdf.extract"
	OpTextCreate OperationType = "text.create"
	OpTextUpdate OperationType = "text.update"
	OpTextDelete OperationType = "text.delete"
)

// AuditEvent es el evento que el orquestador envía al MS AuditLog.
type AuditEvent struct {
	Action      OperationType `json:"action"`
	EntityType  string        `json:"entity_type"`
	Checksum    Checksum      `json:"checksum"`
	Details     any           `json:"details,omitempty"`
	PerformedAt time.Time     `json:"performed_at"`
}

// AuditLog es un evento de auditoría tal como lo devuelve el MS AuditLog.
type AuditLog struct {
	ID          string    `json:"_id"`
	Action      string    `json:"action"`
	EntityType  string    `json:"entity_type"`
	Checksum    Checksum  `json:"checksum"`
	Details     any       `json:"details"`
	PerformedAt time.Time `json:"performed_at"`
}
