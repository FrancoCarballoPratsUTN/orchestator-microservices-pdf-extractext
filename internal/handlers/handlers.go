package handlers

import (
	"encoding/json"
	"net/http"

	"validationmicroservices-pdf-extractext/internal/httpclient"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(httpclient.Problem{
		Type:   "about:blank",
		Title:  title,
		Status: status,
		Detail: detail,
	})
}