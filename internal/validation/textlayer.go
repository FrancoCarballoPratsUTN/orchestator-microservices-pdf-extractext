package validation

import "strings"

// ValidateExtracted rechaza un documento que el Extract devolvió sin una capa de
// texto utilizable (un PDF escaneado). Es un PDF real que este sistema no puede
// procesar, por eso se mapea a 422 y no al 400 de un cuerpo malformado.
func ValidateExtracted(content string) error {
	if strings.TrimSpace(content) == "" {
		return ErrNoExtractableText
	}
	return nil
}
