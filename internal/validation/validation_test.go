package validation

import (
	"errors"
	"testing"
)

func TestPreExtractChecksExtensionBeforeSignature(t *testing.T) {
	t.Parallel()

	_, err := PreExtract(Input{PDF: []byte("no soy un pdf"), Filename: "informe.exe"})
	if !errors.Is(err, ErrUnsupportedExtension) {
		t.Errorf("PreExtract = %v, want ErrUnsupportedExtension checked first", err)
	}
}

func TestPreExtractChecksSignatureWhenFileNameIsAcceptable(t *testing.T) {
	t.Parallel()

	_, err := PreExtract(Input{PDF: []byte("no soy un pdf"), Filename: "informe.pdf"})
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("PreExtract = %v, want ErrInvalidSignature", err)
	}
}
