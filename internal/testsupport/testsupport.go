// Package testsupport centraliza los fixtures compartidos por las suites de
// tests: los PDFs reales del corpus en tests/stress/pdfs. DRY también aplica a
// los tests.
package testsupport

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const (
	// ScrumGuideName es el documento del corpus usado como fixture principal de PDF.
	ScrumGuideName = "2020-Scrum-Guide-Spanish-Latin-South-American.pdf"
	// ScrumGuidePages es su número de páginas, fijado para que los tests puedan
	// verificarlo.
	ScrumGuidePages = 16
)

// CorpusDir devuelve la ruta absoluta de tests/stress/pdfs, resuelta desde este
// archivo para que los llamadores nunca dependan del directorio de trabajo.
func CorpusDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("testsupport: cannot resolve corpus directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "stress", "pdfs")
}

// CorpusPDF lee un PDF del corpus por nombre de archivo.
func CorpusPDF(t *testing.T, name string) []byte {
	t.Helper()

	pdf, err := os.ReadFile(filepath.Join(CorpusDir(), name))
	if err != nil {
		t.Fatalf("testsupport: read corpus PDF %s: %v", name, err)
	}
	return pdf
}

// ScrumGuidePDF devuelve el documento versionado del corpus Scrum Guide
// (16 páginas).
func ScrumGuidePDF(t *testing.T) []byte {
	t.Helper()
	return CorpusPDF(t, ScrumGuideName)
}
