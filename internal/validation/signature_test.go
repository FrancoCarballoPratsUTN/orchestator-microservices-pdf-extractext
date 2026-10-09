package validation

import (
	"errors"
	"testing"
)

func TestCheckSignatureAcceptsPDFHeader(t *testing.T) {
	t.Parallel()

	if err := checkSignature([]byte("%PDF-1.7\n%")); err != nil {
		t.Errorf("checkSignature(valid header) = %v, want nil", err)
	}
}

func TestCheckSignatureRejectsBodiesWithoutPDFHeader(t *testing.T) {
	t.Parallel()

	cases := map[string][]byte{
		"empty body":         nil,
		"plain text":         []byte("no soy un pdf"),
		"html":               []byte("<html></html>"),
		"leading whitespace": []byte("\n%PDF-1.7"),
	}

	for name, body := range cases {
		if err := checkSignature(body); !errors.Is(err, ErrInvalidSignature) {
			t.Errorf("%s: checkSignature = %v, want ErrInvalidSignature", name, err)
		}
	}
}
