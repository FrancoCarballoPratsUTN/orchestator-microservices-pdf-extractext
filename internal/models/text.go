package models

import "time"

// Text es un documento de texto almacenado en el MS Persistence.
type Text struct {
	Checksum  Checksum       `json:"checksum"`
	Text      string         `json:"text"`
	Name      string         `json:"name"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}
