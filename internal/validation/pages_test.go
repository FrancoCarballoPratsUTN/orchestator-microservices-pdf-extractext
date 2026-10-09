package validation

import (
	"errors"
	"testing"
)

func TestPreExtractRejectsPDFAboveThePageLimit(t *testing.T) {
	t.Parallel()

	_, err := PreExtract(Input{PDF: buildPDF(3), Filename: "informe.pdf", MaxPages: 2})
	if !errors.Is(err, ErrTooManyPages) {
		t.Fatalf("PreExtract(3 pages, limit 2) = %v, want ErrTooManyPages", err)
	}
}

func TestPreExtractAcceptsPDFAtExactlyThePageLimit(t *testing.T) {
	t.Parallel()

	result, err := PreExtract(Input{PDF: buildPDF(3), Filename: "informe.pdf", MaxPages: 3})
	if err != nil {
		t.Fatalf("PreExtract(3 pages, limit 3) = %v, want nil", err)
	}
	if result.PageCount != 3 {
		t.Errorf("PageCount = %d, want 3", result.PageCount)
	}
}

func TestPreExtractWithoutPageLimitAcceptsAnyPageCount(t *testing.T) {
	t.Parallel()

	if _, err := PreExtract(Input{PDF: buildPDF(3), Filename: "informe.pdf", MaxPages: 0}); err != nil {
		t.Errorf("PreExtract(MaxPages 0) = %v, want nil (no limit)", err)
	}
}

func TestPreExtractValidatesStructureBeforeThePageLimit(t *testing.T) {
	t.Parallel()

	encrypted := encryptPDF(t, buildPDF(3), "s3cret")

	_, err := PreExtract(Input{PDF: encrypted, Filename: "informe.pdf", MaxPages: 1})
	if !errors.Is(err, ErrEncryptedPDF) {
		t.Errorf("PreExtract(encrypted, limit 1) = %v, want ErrEncryptedPDF before the page gate", err)
	}
}
