// Package validation centralizes every check the orchestrator applies to an
// incoming PDF: before delegating to the Extract service (file name extension,
// %PDF- signature, and later structure, encryption and page count) and after it
// returns (extractable text). Keeping them together means the handler does not
// need to know which layer produced a failure: StatusOf maps any validation
// error to the HTTP status it must answer with.
package validation

import (
	"errors"
	"net/http"
)

// Sentinel errors returned by the validation chain. Callers compare them with
// errors.Is; StatusOf maps them to the HTTP status the handler must return.
var (
	// ErrUnsupportedExtension rejects a file name that does not end in .pdf.
	ErrUnsupportedExtension = errors.New("unsupported file extension")
	// ErrInvalidSignature rejects a body without the %PDF- header.
	ErrInvalidSignature = errors.New("missing %PDF- header")
	// ErrMalformedPDF rejects a body whose internal structure is broken.
	ErrMalformedPDF = errors.New("malformed pdf structure")
	// ErrEncryptedPDF rejects a PDF that requires a password: a real PDF this
	// system cannot process, not a broken one.
	ErrEncryptedPDF = errors.New("pdf is encrypted")
	// ErrTooManyPages rejects a PDF above the configured page limit.
	ErrTooManyPages = errors.New("pdf exceeds the page limit")
	// ErrNoExtractableText rejects a scanned PDF the Extract returned without a
	// text layer.
	ErrNoExtractableText = errors.New("pdf has no extractable text layer")
)

// statusMappings pairs each validation error with the HTTP status it maps to.
// It is the single source of truth for the handler, which no longer needs an
// error switch of its own.
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

// StatusOf reports the HTTP status and the matching RFC 9457 title for a
// validation error, plus whether err belongs to the validation chain at all.
// Errors outside the set return ok == false so the handler can fall back to its
// generic mapping.
func StatusOf(err error) (status int, title string, ok bool) {
	for _, m := range statusMappings {
		if errors.Is(err, m.err) {
			return m.status, http.StatusText(m.status), true
		}
	}
	return 0, "", false
}
