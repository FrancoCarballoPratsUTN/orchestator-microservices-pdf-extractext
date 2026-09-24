package models

import "time"

type OperationType string

const OpPDFExtract OperationType = "pdf.extract"

type AuditEvent struct {
	Action      OperationType `json:"action"`
	EntityType  string        `json:"entity_type"`
	Checksum    Checksum      `json:"checksum"`
	Details     any           `json:"details,omitempty"`
	PerformedAt time.Time     `json:"performed_at"`
}

type AuditLog struct {
	ID          string    `json:"_id"`
	Action      string    `json:"action"`
	EntityType  string    `json:"entity_type"`
	Checksum    Checksum  `json:"checksum"`
	Details     any       `json:"details"`
	PerformedAt time.Time `json:"performed_at"`
}