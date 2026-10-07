package markdown

import "strings"

// escapableChars son los caracteres que, sin escapar, convertirían el texto
// plano del PDF en markdown que el renderer interpreta: énfasis, código inline,
// links, HTML y tablas.
//
// Es un conjunto mínimo y deliberado (YAGNI): no escapamos puntuación como "-" o
// "#", porque escaparla ensuciaría el markdown crudo sin evitar ninguna confusión
// real en un texto en prosa. Escapar de más también tiene costo: la salida tiene
// que seguir siendo legible para quien la inspeccione.
const escapableChars = "\\`*_[]<>|"

// escape antepone una barra invertida a cada carácter de escapableChars.
//
// Recorre el texto una sola vez y decide carácter por carácter, así que un "*"
// produce exactamente un "\*". Nada de Replace encadenados: el doble escapado
// haría que la salida dependiera de cuántas veces se corriera la conversión, y
// esa salida es la que se hashea.
func escape(text string) string {
	if !strings.ContainsAny(text, escapableChars) {
		return text
	}

	var builder strings.Builder
	builder.Grow(len(text) + len(text)/8)

	for _, r := range text {
		if strings.ContainsRune(escapableChars, r) {
			builder.WriteByte('\\')
		}
		builder.WriteRune(r)
	}

	return builder.String()
}
