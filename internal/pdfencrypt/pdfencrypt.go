// Package pdfencrypt genera PDFs protegidos con contraseña.
//
// Existe sólo para construir fixtures de tests y la CLI de desarrollo detrás de
// `make smoke`. El servicio en ejecución nunca cifra ni descifra: la validación
// sólo clasifica un PDF protegido como validation.ErrEncryptedPDF y lo rechaza.
// No se debe agregar lógica de decisión a este paquete.
package pdfencrypt

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// DefaultPassword es la contraseña de usuario/propietario que usan la CLI de
// desarrollo y los fixtures de test.
const DefaultPassword = "smoke-secret"

// Encrypt devuelve una copia de pdf protegida con userPassword mediante AES-256.
// El resultado siempre es un PDF que pdfcpu clasifica como protegido con
// contraseña.
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
