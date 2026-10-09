package validation

import (
	"errors"
	"testing"
)

func TestCheckExtensionAcceptsPDFFileNames(t *testing.T) {
	t.Parallel()

	names := []string{"informe.pdf", "informe.PDF", "docs/informe.pdf", `C:\fakepath\a.pdf`}
	for _, name := range names {
		if err := checkExtension(name); err != nil {
			t.Errorf("checkExtension(%q) = %v, want nil", name, err)
		}
	}
}

func TestCheckExtensionRejectsFileNamesWithoutPDFExtension(t *testing.T) {
	t.Parallel()

	names := []string{"informe.exe", "informe", "a.pdf.exe", "a.pdf.txt"}
	for _, name := range names {
		if err := checkExtension(name); !errors.Is(err, ErrUnsupportedExtension) {
			t.Errorf("checkExtension(%q) = %v, want ErrUnsupportedExtension", name, err)
		}
	}
}

func TestCheckExtensionSkipsEmptyFileName(t *testing.T) {
	t.Parallel()

	if err := checkExtension(""); err != nil {
		t.Errorf("checkExtension(\"\") = %v, want nil", err)
	}
}
