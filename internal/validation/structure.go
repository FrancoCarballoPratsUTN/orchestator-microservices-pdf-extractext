package validation

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpu "github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// analyzePDF parses pdf once with pdfcpu to validate its structure, separate
// password-protected files from genuinely broken ones and count its pages. A
// single parse yields both the verdict and the page count, so the packet is
// never walked twice.
func analyzePDF(pdf []byte) (int, error) {
	pageCount, err := api.PageCount(context.Background(), bytes.NewReader(pdf), pdfcpuConfig())
	if err == nil {
		return pageCount, nil
	}
	if isPasswordProtected(err) {
		return 0, ErrEncryptedPDF
	}
	return 0, fmt.Errorf("%w: %v", ErrMalformedPDF, err)
}

// isPasswordProtected reports whether pdfcpu failed because the PDF needs a
// password, as opposed to being unreadable for any other reason.
func isPasswordProtected(err error) bool {
	return errors.Is(err, pdfcpu.ErrWrongPassword) || errors.Is(err, pdfcpu.ErrEncrypted)
}

// pdfcpuConfig returns a stateless, relaxed configuration. Stateless keeps
// pdfcpu from creating its default config directory (the container filesystem is
// read-only); relaxed avoids rejecting real-world PDFs, since this phase only
// needs to catch genuinely broken files.
func pdfcpuConfig() *model.Configuration {
	conf := model.NewStatelessConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	return conf
}
