// Command encryptpdf escribe una copia de un PDF protegida con contraseña.
//
// Es una herramienta sólo para desarrollo detrás de `make smoke`: construye el
// fixture cifrado que la prueba de humo envía al orquestador, que debe responder
// 422. Producción nunca la usa.
//
// Uso:
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
