package markdown

import "strings"

// minRepeatedPages es cuántas páginas tienen que compartir un renglón de borde
// para que se considere header o pie.
//
// Con 2 alcanza porque es el mínimo que constituye evidencia: una sola aparición
// es contenido normal y borrarla perdería información. Subirlo a 3 dejaría
// sueltos los headers y pies de los documentos de dos páginas, que son comunes.
const minRepeatedPages = 2

// stripRepeatedEdges quita de cada página el header y el pie que se repiten
// textualmente en varias páginas.
//
// Tiene que correr sobre las LÍNEAS, no sobre los párrafos ya aplanados: PDFium
// suele emitir el header pegado al primer renglón del cuerpo sin blanco de
// por medio, así que aplanado pasa a formar parte del párrafo y se vuelve
// indistinguible del contenido.
//
// La repetición tiene que ser exacta. Un pie con número de página variable
// ("Página 1", "Página 2") no se detecta: es conservador a propósito, porque
// borrar contenido es un error visible y dejar un header de más es cosmético.
func stripRepeatedEdges(pages [][]string) [][]string {
	// Con menos de dos páginas no hay repetición que observar, salvo el caso de
	// una página cuyo primer y último renglón coinciden, que no prueba nada.
	if len(pages) < minRepeatedPages {
		return pages
	}

	repeated := repeatedEdges(pages)
	if len(repeated) == 0 {
		return pages
	}

	stripped := make([][]string, 0, len(pages))
	for _, page := range pages {
		stripped = append(stripped, trimRepeatedEdges(page, repeated))
	}
	return stripped
}

// repeatedEdges devuelve los renglones que son borde en TODOS los segmentos con
// contenido suficiente.
//
// El umbral es deliberadamente exigente: no "al menos dos apariciones" sino
// "presente en todos". La razón está en la ambigüedad del separador: un "\n\n"
// dentro de una página es indistinguible de un "\n\n" entre páginas, así que un
// segmento puede ser un fragmento a mitad del documento y sus "bordes" son texto
// corriente. Con un umbral laxo, ese texto corriente se repetía por casualidad y
// se borraba contenido real (ver TestConvertDeletesNothingWhenBlankLinesHidePageBoundaries).
//
// Exigir uniformidad hace que la función falle en seguro: ante una estructura
// ambigua no borra nada y sólo se pierde la limpieza cosmética, nunca texto.
func repeatedEdges(pages [][]string) map[string]bool {
	occurrences := make(map[string]int)
	eligible := 0

	for _, page := range pages {
		indexes := pageEdgeIndexes(page)
		if len(indexes) == 0 {
			continue
		}
		eligible++

		// Un renglón cuenta una vez por segmento, aunque sea borde por los dos
		// extremos, para que el total sea comparable entre segmentos.
		edges := make(map[string]bool, len(indexes))
		for _, index := range indexes {
			edges[page[index]] = true
		}
		for text := range edges {
			occurrences[text]++
		}
	}

	if eligible < minRepeatedPages {
		return nil
	}

	repeated := make(map[string]bool, len(occurrences))
	for text, count := range occurrences {
		if count == eligible {
			repeated[text] = true
		}
	}
	return repeated
}

// pageEdgeIndexes da los índices del primer y del último renglón con contenido.
//
// Si la página tiene un solo renglón con contenido, se devuelve ese único índice
// una sola vez: no hay cuerpo del que quitar nada, y borrarlo dejaría la página en
// blanco. Si no tiene ninguno, no devuelve nada.
func pageEdgeIndexes(page []string) []int {
	var indexes []int

	for index, line := range page {
		if line != "" {
			indexes = append(indexes, index)
		}
	}
	if len(indexes) < minRepeatedPages {
		return nil
	}

	return []int{indexes[0], indexes[len(indexes)-1]}
}

// trimRepeatedEdges quita como máximo un renglón por extremo. El límite evita que
// "X\nX" dentro de una misma página borre su contenido entero.
//
// Los renglones vacíos se conservan: son los que separan párrafos dentro de la
// página y no son bordes de nada.
func trimRepeatedEdges(page []string, repeated map[string]bool) []string {
	indexes := pageEdgeIndexes(page)
	if len(indexes) == 0 {
		return page
	}

	drop := make(map[int]bool, len(indexes))
	for _, index := range indexes {
		if repeated[page[index]] {
			drop[index] = true
		}
	}
	if len(drop) == 0 {
		return page
	}

	kept := make([]string, 0, len(page))
	for index, line := range page {
		if !drop[index] {
			kept = append(kept, line)
		}
	}
	return kept
}

// splitPages parte el contenido en páginas y cada página en renglones.
//
// Los renglones vacíos se conservan porque son los que separan párrafos dentro de
// una página; los bordes se buscan saltándolos. Cada renglón se recorta acá y no
// más adelante: la detección de bordes compara texto exacto y un "\r" o un
// tabulador sobrante la haría fallar.
func splitPages(content string) [][]string {
	var pages [][]string

	for _, segment := range strings.Split(content, pageSeparator) {
		if lines := splitLines(segment); hasContent(lines) {
			pages = append(pages, lines)
		}
	}
	return pages
}

func splitLines(segment string) []string {
	lines := strings.Split(segment, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimSpace(line)
	}
	return lines
}

func hasContent(lines []string) bool {
	for _, line := range lines {
		if line != "" {
			return true
		}
	}
	return false
}
