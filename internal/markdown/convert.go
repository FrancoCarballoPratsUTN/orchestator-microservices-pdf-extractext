// Package markdown convierte el texto plano que devuelve el MS Extract en
// markdown. Es un paquete puro: sin dependencias, sin I/O y sin estado, igual que
// internal/checksum.
//
// El Extract no manda datos de layout por su contrato (sólo un string por
// documento), así que aquí no se detectan títulos ni listas reales: el trabajo es
// cosmético y conservative. Ver tasks/plan.md §7.1.
//
// El determinismo es un requisito duro, no una preferencia: el checksum del
// sistema es el SHA-256 de la salida de esta función, así que la misma entrada
// tiene que producir siempre el mismo string. Nada de time, rand, orden de
// iteración de mapas ni casing dependiente de locale.
package markdown

import (
	"strings"
	"unicode"
)

// pageSeparator separa páginas en el content del Extract.
//
// No es una inferencia: el Extract las une con "\n\n" explícitamente
// (Conversor/internal/infrastructure/pdf/extractor.go:59). Dentro de una página
// PDFium emite un renglón por línea, separados por "\r\n", y los blancos entre
// párrafos vienen como renglones vacíos, no como separadores.
const pageSeparator = "\n\n"

// Convert transforma el texto plano del MS Extract en markdown.
//
// El pipeline es: partir páginas → quitar headers/pies repetidos → aplanar cada
// página en párrafos → decidir título y escapar.
//
// Ese orden no es arbitrario. Los headers/pies se detectan sobre los renglones,
// antes de aplanar, porque PDFium suele pegarlos al cuerpo sin blanco de por
// medio y aplanados se volverían indistinguibles del contenido. El marcado, en
// cambio, sólo puede correr sobre el párrafo ya aplanado, porque un título
// partido en dos renglones tiene que reconocerse como una sola línea.
//
// No normaliza \r\n a propósito: los renglones se recortan con TrimSpace al
// partir la página, y TrimSpace ya descarta el "\r". Una pasada extra sería
// código muerto.
func Convert(content string) string {
	pages := stripRepeatedEdges(splitPages(content))

	paragraphs := make([]string, 0, len(pages))
	for _, page := range pages {
		for _, paragraph := range unwrap(page) {
			paragraphs = append(paragraphs, decorate(paragraph))
		}
	}

	return strings.Join(paragraphs, pageSeparator)
}

// decorate convierte un párrafo de texto plano en markdown. El escapado va antes
// del marcado para que los caracteres del marcador nunca se escapen a sí mismos.
func decorate(paragraph string) string {
	escaped := escape(paragraph)
	if isHeading(paragraph) {
		return headingMarker + escaped
	}
	return escaped
}

// unwrap aplana los renglones de una página en párrafos. El orden importa: la
// des-hifenización tiene que observar el guion pegado a un fin de línea, así que
// corre antes de unir el párrafo.
//
// Ademas separa los titulos: dentro de una pagina el Extract no entrega ninguna
// senal de parrafo (ver la nota de isLoneHeading y los goldens), asi que un
// renglon corto que esta solo no puede ser un parrafo mal envuelto. Sin esto, una
// pagina entera de 90 renglones seeria un solo parrafo.
func unwrap(lines []string) []string {
	paragraphs := make([]string, 0, len(lines))

	for _, group := range splitLoneHeadings(groupParagraphs(lines)) {
		paragraphs = append(paragraphs, dehyphenate(group))
	}
	return paragraphs
}

// maxLoneHeadingWords y fullWidthLine son los dos umbrales de la detección de
// títulos pegados al cuerpo, calibrados contra los PDFs reales de testdata.
//
// Los seis renglones salen de los títulos de esos documentos (de una a cinco
// palabras). El ancho de 60 sale de su distribución: sobre 5211 renglones, la
// mediana es 62 y el 55% mide 60 o más.
const (
	maxLoneHeadingWords = 6
	fullWidthLine       = 60
)

// isLoneHeading indica que el grupo empieza con un título pegado al cuerpo, y en
// qué renglón cortarlo.
//
// Existe porque los PDFs reales del testdata llegan del Extract sin ningún renglón
// en blanco entre el título y el párrafo (el de Kanban, de 90 páginas, tiene cero)
// y PDFium no entrega la posición vertical que sí los distinguiría. Sin esto una
// página entera se aplana en un solo párrafo y el título queda fundido con el
// texto.
//
// La señal disponible es el ancho: un título no se envuelve a lo ancho de la
// página, un párrafo sí. Por eso el criterio exige que el renglón *siguiente* sea
// de ancho completo; si también es corto, el renglon corto es parte del mismo
// bloque y no se corta nada.
//
// Ese detalle importa en los datos reales. La primera página del Scrum Guide
// empieza con "1" (el número de página), seguido de "Propósito de la Guía Scrum" y
// del cuerpo. Sólo con "el primer renglón es corto" se habría partido después del
// 1, dejando el título verdadero pegado al párrafo. Con la regla del ancho se
// corta después de "Propósito de la Guía Scrum", que es lo correcto.
//
// Devuelve el índice del primer renglón del cuerpo, o -1 si el grupo no empieza
// con un título.
func loneHeadingSplit(group []string) int {
	for i := 0; i+1 < len(group); i++ {
		if len(strings.Fields(group[i])) > maxLoneHeadingWords {
			// un renglón largo empieza el cuerpo: ya no hay título por cortar
			return -1
		}
		if len(group[i+1]) >= fullWidthLine {
			return i + 1
		}
	}
	return -1
}

// splitLoneHeadings parte un grupo justo después de un título que estaba pegado
// al cuerpo. Los grupos sin título se devuelven sin tocar.
func splitLoneHeadings(groups [][]string) [][]string {
	split := make([][]string, 0, len(groups)*2)

	for _, group := range groups {
		at := loneHeadingSplit(group)
		if at < 0 {
			split = append(split, group)
			continue
		}
		split = append(split, group[:at], group[at:])
	}
	return split
}

// groupParagraphs agrupa líneas consecutivas sin blancos intermedios, recortando
// cada línea. Es también el único filtro de contenido vacío: una página en blanco
// no produce ningún grupo y por lo tanto ningún párrafo.
func groupParagraphs(lines []string) [][]string {
	var groups [][]string

	var current []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(current) > 0 {
				groups = append(groups, current)
				current = nil
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	return groups
}

// dehyphenate une las líneas de un párrafo con un espacio, salvo cuando la línea
// termina en guion y la siguiente arranca en minúscula: en ese caso es una
// palabra partida por el corte de línea de PDFium y hay que pegarla sin guion.
//
// La condición de minúscula es lo que evita falsos positivos. "página 3 -\nindice"
// no es una palabra partida, y un guion seguido de mayúscula suele ser una
// enumeración o un título, no una continuación de palabra.
func dehyphenate(lines []string) string {
	paragraphs := make([]string, 0, len(lines))
	pending := ""

	for _, line := range lines {
		switch {
		case pending == "":
			pending = line
		case continuesWord(pending, line):
			pending = strings.TrimSuffix(pending, "-") + line
		default:
			paragraphs = append(paragraphs, pending)
			pending = line
		}
	}
	if pending != "" {
		paragraphs = append(paragraphs, pending)
	}

	return normalizeSpaces(strings.Join(paragraphs, " "))
}

// continuesWord indica que previous corta una palabra que line continúa.
func continuesWord(previous, line string) bool {
	if !strings.HasSuffix(previous, "-") {
		return false
	}
	if strings.HasSuffix(previous, "--") {
		return false
	}

	for _, r := range line {
		return unicode.IsLower(r)
	}
	return false
}

// normalizeSpaces colapsa corridas de espacios en un solo espacio.
//
// Usa unicode.IsSpace y no una comparación con ' ' y '\t'. PDFium emite espacios
// duros (U+00A0) dentro de las líneas, y no sólo en los bordes: el golden de
// Essential Kanban tenía seis, en cosas como "(ver Fig 14)" y "Large and
// multiple services". TrimSpace ya limpiaba los bordes de cada renglón; lo que
// quedaba era el NBSP interno, invisible en cualquier revisión visual y
// responsable de que el markdown que el cliente persiste llevara caracteres de
// espacio que nadie pidió.
//
// unicode.IsSpace cubre U+00A0, U+2000-U+200A, U+202F, U+205F y U+3000, además
// de ' ', '\t', '\n' y '\r', así que un PDF con cualquier otra variante Unicode
// de espacio tampoco la filtra.
func normalizeSpaces(text string) string {
	var builder strings.Builder
	space := false

	for _, r := range text {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && builder.Len() > 0 {
			builder.WriteRune(' ')
		}
		space = false
		builder.WriteRune(r)
	}
	return builder.String()
}
