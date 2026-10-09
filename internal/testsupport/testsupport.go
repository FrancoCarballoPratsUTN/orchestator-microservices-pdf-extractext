// Package testsupport centralizes fixtures shared by the test suites: the real
// corpus PDFs under tests/stress/pdfs. DRY applies to tests too.
package testsupport

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const (
	// ScrumGuideName is the corpus document used as the main PDF fixture.
	ScrumGuideName = "2020-Scrum-Guide-Spanish-Latin-South-American.pdf"
	// ScrumGuidePages is its page count, pinned so tests can assert it.
	ScrumGuidePages = 16
)

// CorpusDir returns the absolute path of tests/stress/pdfs, resolved from this
// file so callers never depend on the working directory.
func CorpusDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("testsupport: cannot resolve corpus directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "stress", "pdfs")
}

// CorpusPDF reads a PDF from the corpus by file name.
func CorpusPDF(t *testing.T, name string) []byte {
	t.Helper()

	pdf, err := os.ReadFile(filepath.Join(CorpusDir(), name))
	if err != nil {
		t.Fatalf("testsupport: read corpus PDF %s: %v", name, err)
	}
	return pdf
}

// ScrumGuidePDF returns the versioned Scrum Guide corpus document (16 pages).
func ScrumGuidePDF(t *testing.T) []byte {
	t.Helper()
	return CorpusPDF(t, ScrumGuideName)
}
