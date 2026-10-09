package validation

import (
	"bytes"
	"fmt"
)

// pdfSignature is the header every PDF file starts with.
var pdfSignature = []byte("%PDF-")

// checkSignature rejects a body that does not start with the %PDF- header. It is
// the cheapest possible content check and must match at the very start: a reader
// looks for the marker at the beginning of the file, not anywhere in it.
func checkSignature(pdf []byte) error {
	if bytes.HasPrefix(pdf, pdfSignature) {
		return nil
	}
	return fmt.Errorf("%w: body does not start with %%PDF-", ErrInvalidSignature)
}
