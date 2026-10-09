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

// ExtractPDFResponse es lo que devuelve el orquestador a su cliente. Text es el
// contenido que entrega el Extract tal cual (ya viene formateado) o el registro
// almacenado en un cache hit, y PageCount siempre viene de pdfcpu. Checksum es el
// SHA-256 de los bytes del PDF subido: es la clave de dedup del sistema, no del
// texto (checksum == SHA-256(bytes del PDF)).
type ExtractPDFResponse struct {
	Checksum  models.Checksum `json:"checksum"`
	PageCount int             `json:"page_count"`
	Text      string          `json:"text"`
}
