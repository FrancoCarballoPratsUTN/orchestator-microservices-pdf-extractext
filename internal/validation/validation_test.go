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

func TestPreExtractAcceptsBodyThatPassesTheCurrentChain(t *testing.T) {
	t.Parallel()

	// The chain is still extension + signature here; structure, encryption and
	// page count arrive in a later task, so a %PDF- body is enough for now.
	if _, err := PreExtract(Input{PDF: []byte("%PDF-1.7"), Filename: "informe.pdf"}); err != nil {
		t.Errorf("PreExtract(valid) = %v, want nil", err)
	}
}
