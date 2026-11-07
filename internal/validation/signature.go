package validation

import (
	"bytes"
	"fmt"
)

// pdfSignature es la cabecera con la que empieza todo archivo PDF.
var pdfSignature = []byte("%PDF-")

// checkSignature rechaza un cuerpo que no empieza con la cabecera %PDF-. Es la
// comprobación de contenido más barata posible y debe coincidir al inicio: un
// lector busca el marcador al principio del archivo, no en cualquier parte.
func checkSignature(pdf []byte) error {
	if bytes.HasPrefix(pdf, pdfSignature) {
		return nil
	}
	return fmt.Errorf("%w: body does not start with %%PDF-", ErrInvalidSignature)
}
