package dto

// ExtractedPage refleja cada página devuelta por el MS Extract.
type ExtractedPage struct {
	PageNumber int    `json:"page_number"`
	Text       string `json:"text"`
}

// ExtractedDocument es la forma exacta de la respuesta 200 del MS Extract.
type ExtractedDocument struct {
	PageCount  int             `json:"page_count"`
	Pages      []ExtractedPage `json:"pages"`
	Text       string          `json:"text"`
	DurationMs uint64          `json:"duration_ms"`
}