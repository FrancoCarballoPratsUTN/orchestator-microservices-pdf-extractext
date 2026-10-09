// Command encryptpdf writes a password-protected copy of a PDF.
//
// Dev-only helper behind `make smoke`: it builds the encrypted fixture the
// smoke test posts to the orchestrator, which must answer 422. Production never
// uses it.
//
// Usage:
//
//	encryptpdf <in.pdf> <out.pdf> [password]
package main

import (
	"fmt"
	"os"

	"validationmicroservices-pdf-extractext/internal/pdfencrypt"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "encryptpdf:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 || len(args) > 3 {
		return fmt.Errorf("usage: encryptpdf <in.pdf> <out.pdf> [password]")
	}

	password := pdfencrypt.DefaultPassword
	if len(args) == 3 {
		password = args[2]
	}

	pdf, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read %s: %w", args[0], err)
	}

	encrypted, err := pdfencrypt.Encrypt(pdf, password)
	if err != nil {
		return err
	}

	if err := os.WriteFile(args[1], encrypted, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", args[1], err)
	}
	return nil
}
