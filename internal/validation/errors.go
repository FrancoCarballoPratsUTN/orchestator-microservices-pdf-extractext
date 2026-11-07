// Package validation centraliza todas las comprobaciones que el orquestador
// aplica a un PDF entrante: antes de delegar en el servicio Extract (extensión
// del nombre de archivo, firma %PDF- y, más adelante, estructura, cifrado y
// número de páginas) y después de que responde (texto extraíble). Mantenerlas
// juntas hace que el handler no necesite saber qué capa produjo el fallo:
// StatusOf mapea cualquier error de validación al status HTTP con el que debe
// responder.
package validation

import (
	"errors"
	"net/http"
)

// Errores centinela devueltos por la cadena de validación. Los llamadores los
// comparan con errors.Is; StatusOf los mapea al status HTTP que debe devolver
// el handler.
var (
	// ErrUnsupportedExtension rechaza un nombre de archivo que no termina en .pdf.
	ErrUnsupportedExtension = errors.New("unsupported file extension")
	// ErrInvalidSignature rechaza un cuerpo sin la cabecera %PDF-.
	ErrInvalidSignature = errors.New("missing %PDF- header")
	// ErrMalformedPDF rechaza un cuerpo cuya estructura interna está dañada.
	ErrMalformedPDF = errors.New("malformed pdf structure")
	// ErrEncryptedPDF rechaza un PDF que requiere contraseña: un PDF real que
	// este sistema no puede procesar, no uno dañado.
	ErrEncryptedPDF = errors.New("pdf is encrypted")
	// ErrTooManyPages rechaza un PDF por encima del límite de páginas configurado.
	ErrTooManyPages = errors.New("pdf exceeds the page limit")
	// ErrNoExtractableText rechaza un PDF escaneado que el Extract devolvió sin
	// capa de texto.
	ErrNoExtractableText = errors.New("pdf has no extractable text layer")
)

// statusMappings empareja cada error de validación con el status HTTP al que se
// mapea. Es la única fuente de verdad para el handler, que ya no necesita su
// propio switch de errores.
var statusMappings = []struct {
	err    error
	status int
}{
	{ErrUnsupportedExtension, http.StatusUnsupportedMediaType},
	{ErrInvalidSignature, http.StatusBadRequest},
	{ErrMalformedPDF, http.StatusBadRequest},
	{ErrEncryptedPDF, http.StatusUnprocessableEntity},
	{ErrTooManyPages, http.StatusUnprocessableEntity},
	{ErrNoExtractableText, http.StatusUnprocessableEntity},
}

// StatusOf informa el status HTTP y el título RFC 9457 correspondiente a un
// error de validación, además de si err pertenece a la cadena de validación. Los
// errores fuera del conjunto devuelven ok == false para que el handler pueda
// recurrir a su mapeo genérico.
func StatusOf(err error) (status int, title string, ok bool) {
	for _, m := range statusMappings {
		if errors.Is(err, m.err) {
			return m.status, http.StatusText(m.status), true
		}
	}
	return 0, "", false
}
