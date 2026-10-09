package validation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"validationmicroservices-pdf-extractext/internal/pdfencrypt"
	"validationmicroservices-pdf-extractext/internal/testsupport"
)

func TestPreExtractAcceptsStructurallyValidPDF(t *testing.T) {
	t.Parallel()

	if _, err := PreExtract(Input{PDF: testsupport.ScrumGuidePDF(t), Filename: "informe.pdf"}); err != nil {
		t.Fatalf("PreExtract(valid) = %v, want nil", err)
	}
}

func TestPreExtractCountsPages(t *testing.T) {
	t.Parallel()

	result, err := PreExtract(Input{PDF: testsupport.ScrumGuidePDF(t), Filename: "informe.pdf"})
	if err != nil {
		t.Fatalf("PreExtract = %v, want nil", err)
	}
	if result.PageCount != testsupport.ScrumGuidePages {
		t.Errorf("PageCount = %d, want %d", result.PageCount, testsupport.ScrumGuidePages)
	}
}

func TestPreExtractRejectsTruncatedPDF(t *testing.T) {
	t.Parallel()

	// pdfcpu rebuilds a missing xref, so a shallow cut is still recoverable;
	// cutting into the page objects is what makes it genuinely unreadable.
	pdf := testsupport.ScrumGuidePDF(t)
	if _, err := PreExtract(Input{PDF: pdf[:len(pdf)/3]}); !errors.Is(err, ErrMalformedPDF) {
		t.Errorf("PreExtract(truncated) = %v, want ErrMalformedPDF", err)
	}
}

func TestPreExtractRejectsGarbageAfterValidHeader(t *testing.T) {
	t.Parallel()

	_, err := PreExtract(Input{PDF: []byte("%PDF-1.7\nthis is not a pdf body")})
	if !errors.Is(err, ErrMalformedPDF) {
		t.Errorf("PreExtract(garbage) = %v, want ErrMalformedPDF", err)
	}
}

func TestPreExtractClassifiesPasswordProtectedPDFAsEncrypted(t *testing.T) {
	t.Parallel()

	encrypted, err := pdfencrypt.Encrypt(testsupport.ScrumGuidePDF(t), pdfencrypt.DefaultPassword)
	if err != nil {
		t.Fatalf("pdfencrypt.Encrypt: %v", err)
	}

	_, err = PreExtract(Input{PDF: encrypted, Filename: "informe.pdf"})
	if !errors.Is(err, ErrEncryptedPDF) {
		t.Fatalf("PreExtract(encrypted) = %v, want ErrEncryptedPDF", err)
	}
	if errors.Is(err, ErrMalformedPDF) {
		t.Error("an encrypted PDF must not be classified as malformed")
	}
}

// TestPreExtractDoesNotFlagRealCorpusPDFs is the false-positive gate: if pdfcpu
// rejects a real, readable PDF, this phase is not done, no matter how many
// synthetic cases pass.
func TestPreExtractDoesNotFlagRealCorpusPDFs(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join(testsupport.CorpusDir(), "*.pdf"))
	if err != nil {
		t.Fatalf("glob corpus: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no corpus PDFs found; expected versioned files under tests/stress/pdfs")
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()

			pdf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			result, err := PreExtract(Input{PDF: pdf, Filename: filepath.Base(path)})
			if err != nil {
				t.Errorf("PreExtract(%s) = %v, want nil (false positive)", path, err)
			}
			if result.PageCount <= 0 {
				t.Errorf("PreExtract(%s) PageCount = %d, want > 0", path, result.PageCount)
			}
		})
	}
}
