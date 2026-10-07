package markdown

import (
	"strings"
	"unicode"
)

const (
	// headingMarker abre los títulos que la heurística detecta. Nivel 2 porque
	// no hay forma de saber si en el PDF original era un título de primer nivel.
	headingMarker = "## "

	// maxHeadingWords y maxHeadingLength separan un título de una frase escrita
	// en mayúsculas. Son la única defensa contra el falso positivo principal:
	// "ATENCION EL CLIENTE EXIGE UNA REUNION URGENTE PARA REVISAR EL ALCANCE
	// DEL PROYECTO" son mayúsculas, pero es un grito, no un título.
	//
	// Son constantes y no configuración (YAGNI): la conversión tiene que ser
	// determinista y reproducible, y un umbral ajustable por entorno haría que
	// el checksum dependiera del despliegue.
	maxHeadingWords  = 12
	maxHeadingLength = 120
)

// isHeading decide si un párrafo ya aplanado es un título.
//
// El Extract no entrega tamaño de fuente ni posición, así que no hay layout que
// consultar: la única señal disponible es que el párrafo esté entero en
// mayúsculas y sea corto. Es una heurística cosmética, no una reconstrucción del
// documento.
func isHeading(text string) bool {
	if len(text) > maxHeadingLength {
		return false
	}
	if len(strings.Fields(text)) > maxHeadingWords {
		return false
	}

	letters, uppercase := 0, 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.IsUpper(r) {
			uppercase++
		}
	}

	return letters > 0 && letters == uppercase
}
