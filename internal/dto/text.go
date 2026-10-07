package dto

import "validationmicroservices-pdf-extractext/internal/models"

type CreateTextRequest struct {
	Text     string          `json:"text"`
	Checksum models.Checksum `json:"checksum"`
	Name     string          `json:"name"`
	Metadata map[string]any  `json:"metadata"`
}

type CreateTextResponse struct {
	Message  string          `json:"message"`
	Checksum models.Checksum `json:"checksum"`
}

// UpdateTextRequest modela un update parcial. Los punteros y mapas nil se
// serializan como JSON null, que Persistence interpreta como "sin cambio"
// (campo ausente); un valor presente y vacío ("", {}) limpia el campo. Se
// omiten deliberadamente los tags omitempty para no confundir ambos casos.
type UpdateTextRequest struct {
	Name     *string        `json:"name"`
	Metadata map[string]any `json:"metadata"`
}

type DeleteTextResponse struct {
	Message  string          `json:"message"`
	Checksum models.Checksum `json:"checksum"`
}

type CreateTextPayload = CreateTextRequest

type UpdateTextPayload = UpdateTextRequest
