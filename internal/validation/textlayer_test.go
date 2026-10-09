package validation

import (
	"errors"
	"testing"
)

func TestValidateExtractedAcceptsContentWithText(t *testing.T) {
	t.Parallel()

	if err := ValidateExtracted("contenido del Extract"); err != nil {
		t.Errorf("ValidateExtracted(text) = %v, want nil", err)
	}
}

func TestValidateExtractedRejectsBlankContent(t *testing.T) {
	t.Parallel()

	contents := []string{"", "   ", "\n\n", "\r\n \t \n"}
	for _, content := range contents {
		if err := ValidateExtracted(content); !errors.Is(err, ErrNoExtractableText) {
			t.Errorf("ValidateExtracted(%q) = %v, want ErrNoExtractableText", content, err)
		}
	}
}
