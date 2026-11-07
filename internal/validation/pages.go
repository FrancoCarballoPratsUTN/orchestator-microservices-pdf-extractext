package validation

import "fmt"

// checkPageLimit rechaza un PDF cuyo número de páginas supera el techo
// configurado. Un maxPages de cero desactiva el límite, así que la restricción
// sólo existe cuando un operador la configura.
func checkPageLimit(pageCount, maxPages int) error {
	if maxPages > 0 && pageCount > maxPages {
		return fmt.Errorf("%w: %d pages, limit %d", ErrTooManyPages, pageCount, maxPages)
	}
	return nil
}
