package httpapi

import (
	"encoding/json"
	"net/http"

	"validationmicroservices-pdf-extractext/internal/httpclient"
)

// WriteJSON escribe body como JSON con el status indicado.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// WriteProblem escribe un problem details RFC 9457 con el status, título y
// detalle indicados.
func WriteProblem(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(httpclient.Problem{
		Type:   "about:blank",
		Title:  title,
		Status: status,
		Detail: detail,
	})
}
