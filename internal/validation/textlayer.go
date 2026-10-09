package validation

import "strings"

// ValidateExtracted rejects a document the Extract returned without a usable
// text layer (a scanned PDF). It is a real PDF this system cannot process, so it
// maps to 422, not to the 400 of a malformed body.
func ValidateExtracted(content string) error {
	if strings.TrimSpace(content) == "" {
		return ErrNoExtractableText
	}
	return nil
}
