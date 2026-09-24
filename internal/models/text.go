package models

import "time"

type Text struct {
	Checksum  Checksum       `json:"checksum"`
	Text      string         `json:"text"`
	Name      string         `json:"name"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}