package dto

import "validationmicroservices-pdf-extractext/internal/models"

// CreateTextRequest es el cuerpo de POST /api/v1/texts.
type CreateTextRequest struct {
	Text     string          `json:"text"`
	Checksum models.Checksum `json:"checksum"`
	Name     string          `json:"name"`
	Metadata map[string]any  `json:"metadata"`
}

// CreateTextResponse es la respuesta de POST /api/v1/texts.
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

// DeleteTextResponse es la respuesta de DELETE /api/v1/texts/{checksum}.
type DeleteTextResponse struct {
	Message  string          `json:"message"`
	Checksum models.Checksum `json:"checksum"`
}

// CreateTextPayload es el payload que se envía a Persistence al crear un texto.
type CreateTextPayload = CreateTextRequest

// UpdateTextPayload es el payload que se envía a Persistence al actualizar un texto.
type UpdateTextPayload = UpdateTextRequest
