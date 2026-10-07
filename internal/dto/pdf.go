package dto

import "validationmicroservices-pdf-extractext/internal/models"

// ExtractedDocument refleja el contrato del MS Extract. El servicio devuelve
// {content, page_count} y nada más: no expone páginas ni duración, así que el
// DTO tampoco los inventa. Renombrar un tag sin cambiar el nombre del campo
// rompe en silencio, porque encoding/json ignora las claves que no conoce.
type ExtractedDocument struct {
	PageCount int    `json:"page_count"`
	Content   string `json:"content"`
}

// ExtractPDFResponse es lo que devuelve el orquestador a su cliente. Text
// contiene markdown, y Checksum es su SHA-256: el invariante del sistema es
// checksum == SHA-256(text).
type ExtractPDFResponse struct {
	Checksum  models.Checksum `json:"checksum"`
	PageCount int             `json:"page_count"`
	Text      string          `json:"text"`
}
