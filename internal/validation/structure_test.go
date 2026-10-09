package validation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"validationmicroservices-pdf-extractext/internal/testpdf"
)

func TestPreExtractAcceptsStructurallyValidPDF(t *testing.T) {
	t.Parallel()

	if _, err := PreExtract(Input{PDF: testpdf.Build(1), Filename: "informe.pdf"}); err != nil {
		t.Fatalf("PreExtract(valid) = %v, want nil", err)
	}
}

func TestPreExtractCountsPages(t *testing.T) {
	t.Parallel()

	const pages = 3
	result, err := PreExtract(Input{PDF: testpdf.Build(pages), Filename: "informe.pdf"})
	if err != nil {
		t.Fatalf("PreExtract = %v, want nil", err)
	}
	if result.PageCount != pages {
		t.Errorf("PageCount = %d, want %d", result.PageCount, pages)
	}
}

func TestPreExtractRejectsTruncatedPDF(t *testing.T) {
	t.Parallel()

	// pdfcpu rebuilds a missing xref, so a shallow cut is still recoverable;
	// cutting into the page objects is what makes it genuinely unreadable.
	pdf := testpdf.Build(2)
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

	encrypted := encryptPDF(t, testpdf.Build(1), "s3cret")

	_, err := PreExtract(Input{PDF: encrypted, Filename: "informe.pdf"})
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

	paths, err := filepath.Glob(filepath.Join("..", "..", "tests", "stress", "pdfs", "*.pdf"))
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

// encryptPDF encrypts pdf in memory with a user password, so the encrypted
// fixture never has to be committed as a binary.
func encryptPDF(t *testing.T, pdf []byte, userPassword string) []byte {
	t.Helper()

	conf := model.NewStatelessConfiguration()
	conf.UserPW = userPassword
	conf.OwnerPW = userPassword
	conf.EncryptUsingAES = true
	conf.EncryptKeyLength = 256

	var out bytes.Buffer
	if err := api.Encrypt(context.Background(), bytes.NewReader(pdf), &out, conf); err != nil {
		t.Fatalf("api.Encrypt: %v", err)
	}
	return out.Bytes()
}
