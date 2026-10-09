package validation

import (
	"errors"
	"testing"

	"validationmicroservices-pdf-extractext/internal/pdfencrypt"
	"validationmicroservices-pdf-extractext/internal/testsupport"
)

func TestPreExtractRejectsPDFAboveThePageLimit(t *testing.T) {
	t.Parallel()

	_, err := PreExtract(Input{PDF: testsupport.ScrumGuidePDF(t), Filename: "informe.pdf", MaxPages: testsupport.ScrumGuidePages - 1})
	if !errors.Is(err, ErrTooManyPages) {
		t.Fatalf("PreExtract(%d pages, limit %d) = %v, want ErrTooManyPages",
			testsupport.ScrumGuidePages, testsupport.ScrumGuidePages-1, err)
	}
}

func TestPreExtractAcceptsPDFAtExactlyThePageLimit(t *testing.T) {
	t.Parallel()

	result, err := PreExtract(Input{PDF: testsupport.ScrumGuidePDF(t), Filename: "informe.pdf", MaxPages: testsupport.ScrumGuidePages})
	if err != nil {
		t.Fatalf("PreExtract(%d pages, limit %d) = %v, want nil", testsupport.ScrumGuidePages, testsupport.ScrumGuidePages, err)
	}
	if result.PageCount != testsupport.ScrumGuidePages {
		t.Errorf("PageCount = %d, want %d", result.PageCount, testsupport.ScrumGuidePages)
	}
}

func TestPreExtractWithoutPageLimitAcceptsAnyPageCount(t *testing.T) {
	t.Parallel()

	if _, err := PreExtract(Input{PDF: testsupport.ScrumGuidePDF(t), Filename: "informe.pdf", MaxPages: 0}); err != nil {
		t.Errorf("PreExtract(MaxPages 0) = %v, want nil (no limit)", err)
	}
}

func TestPreExtractValidatesStructureBeforeThePageLimit(t *testing.T) {
	t.Parallel()

	encrypted, err := pdfencrypt.Encrypt(testsupport.ScrumGuidePDF(t), pdfencrypt.DefaultPassword)
	if err != nil {
		t.Fatalf("pdfencrypt.Encrypt: %v", err)
	}

	_, err = PreExtract(Input{PDF: encrypted, Filename: "informe.pdf", MaxPages: 1})
	if !errors.Is(err, ErrEncryptedPDF) {
		t.Errorf("PreExtract(encrypted, limit 1) = %v, want ErrEncryptedPDF before the page gate", err)
	}
}
