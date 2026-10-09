package validation

import "fmt"

// checkPageLimit rejects a PDF whose page count is above the configured ceiling.
// A maxPages of zero disables the gate, so the limit only exists when an operator
// configures one.
func checkPageLimit(pageCount, maxPages int) error {
	if maxPages > 0 && pageCount > maxPages {
		return fmt.Errorf("%w: %d pages, limit %d", ErrTooManyPages, pageCount, maxPages)
	}
	return nil
}
