package httpclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Problem representa un problem details RFC 9457 devuelto por un servicio
// upstream.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

// Error implementa la interfaz error para poder propagar el Problem.
func (p Problem) Error() string {
	return fmt.Sprintf("%s: %s (status %d)", p.Title, p.Detail, p.Status)
}

// ParseProblem decodifica un problem details RFC 9457 desde body.
func ParseProblem(body io.Reader) (Problem, error) {
	var problem Problem
	if err := json.NewDecoder(body).Decode(&problem); err != nil {
		return Problem{}, fmt.Errorf("decode problem details: %w", err)
	}
	return problem, nil
}

// StatusError conserva el status HTTP de un error upstream cuyo cuerpo NO es un
// Problem RFC 9457 (por ejemplo, el 503 en texto plano de un circuit breaker de
// Traefik). Existe para que los handlers puedan distinguir el status de un error
// de transporte.
type StatusError struct {
	Code int
	Path string
}

// Error implementa la interfaz error para poder propagar el StatusError.
func (e StatusError) Error() string {
	return fmt.Sprintf("unexpected status %d from %s", e.Code, e.Path)
}

// StatusCode informa el status HTTP contenido en err, ya sea un StatusError
// (cuerpo upstream no RFC 9457) o un Problem parseado (RFC 9457). Los llamadores
// lo usan para preservar los status upstream a través del límite del servicio.
func StatusCode(err error) (int, bool) {
	var statusErr StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Code, true
	}
	var problem Problem
	if errors.As(err, &problem) {
		return problem.Status, true
	}
	return 0, false
}

// IsUnauthorized indica si err corresponde a un 401 upstream. Permite distinguir
// un token mal configurado (error de cableado) de un fallo transitorio.
func IsUnauthorized(err error) bool {
	var problem Problem
	return errors.As(err, &problem) && problem.Status == http.StatusUnauthorized
}

// IsNotFound indica si err corresponde a un 404 upstream. La búsqueda de dedup
// lo usa para distinguir "todavía no almacenado" (miss) de "Persistence está
// fallando" (fail-open).
func IsNotFound(err error) bool {
	var problem Problem
	return errors.As(err, &problem) && problem.Status == http.StatusNotFound
}
