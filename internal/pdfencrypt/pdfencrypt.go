// Package pdfencrypt produces password-protected PDFs.
//
// It exists only to build fixtures for tests and the dev CLI behind
// `make smoke`. The running service never encrypts nor decrypts: validation
// only classifies a password-protected PDF as validation.ErrEncryptedPDF and
// rejects it. Keep any decision logic out of this package.
package pdfencrypt

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// DefaultPassword is the user/owner password used by the dev CLI and the test
// fixtures.
const DefaultPassword = "smoke-secret"

// Encrypt returns pdf protected with userPassword using AES-256. The result is
// always a PDF that pdfcpu classifies as password-protected.
func Encrypt(pdf []byte, userPassword string) ([]byte, error) {
	if userPassword == "" {
		return nil, errors.New("pdfencrypt: user password must not be empty")
	}

	conf := model.NewStatelessConfiguration()
	conf.UserPW = userPassword
	conf.OwnerPW = userPassword
	conf.EncryptUsingAES = true
	conf.EncryptKeyLength = 256

	var out bytes.Buffer
	if err := api.Encrypt(context.Background(), bytes.NewReader(pdf), &out, conf); err != nil {
		return nil, fmt.Errorf("pdfencrypt: encrypt: %w", err)
	}
	return out.Bytes(), nil
}
