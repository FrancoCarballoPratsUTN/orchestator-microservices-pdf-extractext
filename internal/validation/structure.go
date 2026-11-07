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

// analyzePDF parsea pdf una sola vez con pdfcpu para validar su estructura,
// separar los archivos protegidos con contraseña de los realmente dañados y
// contar sus páginas. Un único parseo produce el veredicto y el número de
// páginas, así que el documento nunca se recorre dos veces.
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

// isPasswordProtected indica si pdfcpu falló porque el PDF necesita contraseña,
// en lugar de ser ilegible por cualquier otro motivo.
func isPasswordProtected(err error) bool {
	return errors.Is(err, pdfcpu.ErrWrongPassword) || errors.Is(err, pdfcpu.ErrEncrypted)
}

// pdfcpuConfig devuelve una configuración sin estado y relajada. Sin estado
// evita que pdfcpu cree su directorio de configuración por defecto (el sistema
// de archivos del contenedor es de sólo lectura); relajada evita rechazar PDFs
// del mundo real, ya que esta fase sólo necesita detectar archivos realmente
// dañados.
func pdfcpuConfig() *model.Configuration {
	conf := model.NewStatelessConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	return conf
}
