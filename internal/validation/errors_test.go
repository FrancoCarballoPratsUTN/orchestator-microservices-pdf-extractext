package validation

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestStatusOfMapsValidationErrorsToHTTPStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"unsupported extension", ErrUnsupportedExtension, http.StatusUnsupportedMediaType},
		{"invalid signature", ErrInvalidSignature, http.StatusBadRequest},
		{"malformed pdf", ErrMalformedPDF, http.StatusBadRequest},
		{"encrypted pdf", ErrEncryptedPDF, http.StatusUnprocessableEntity},
		{"too many pages", ErrTooManyPages, http.StatusUnprocessableEntity},
		{"no extractable text", ErrNoExtractableText, http.StatusUnprocessableEntity},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			status, title, ok := StatusOf(tc.err)
			if !ok {
				t.Fatalf("StatusOf(%v) ok = false, want true", tc.err)
			}
			if status != tc.want {
				t.Errorf("status = %d, want %d", status, tc.want)
			}
			if wantTitle := http.StatusText(tc.want); title != wantTitle {
				t.Errorf("title = %q, want %q", title, wantTitle)
			}
		})
	}
}

func TestStatusOfRecognizesWrappedValidationErrors(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("filename check: %w", ErrUnsupportedExtension)

	status, _, ok := StatusOf(err)
	if !ok {
		t.Fatalf("StatusOf(wrapped) ok = false, want true")
	}
	if status != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want %d", status, http.StatusUnsupportedMediaType)
	}
}

func TestStatusOfReportsErrorsOutsideTheValidationSet(t *testing.T) {
	t.Parallel()

	for _, err := range []error{nil, errors.New("database is down")} {
		if _, _, ok := StatusOf(err); ok {
			t.Errorf("StatusOf(%v) ok = true, want false", err)
		}
	}
}
