package validation

import (
	"fmt"
	"path/filepath"
	"strings"
)

// checkExtension rechaza un nombre de archivo cuyo último segmento de ruta no
// termina en .pdf (sin distinguir mayúsculas). El endpoint recibe bytes crudos,
// así que el nombre es metadato opcional: un nombre vacío omite la comprobación.
func checkExtension(filename string) error {
	if filename == "" {
		return nil
	}
	if strings.EqualFold(filepath.Ext(lastPathSegment(filename)), ".pdf") {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrUnsupportedExtension, filename)
}

// lastPathSegment devuelve la porción posterior al último separador, normalizando
// ambos estilos de barra para tratar una ruta de Windows como una de POSIX.
func lastPathSegment(filename string) string {
	if i := strings.LastIndexAny(filename, `/\`); i >= 0 {
		return filename[i+1:]
	}
	return filename
}
