package markdown

import (
	"strings"
	"testing"
)

// Los fixtures respetan el formato real del Extract
// (Conversor/internal/infrastructure/pdf/extractor.go:59): las páginas se unen
// con "\n\n" y dentro de cada página PDFium emite un renglón por línea.
//
// Hay una ambigüedad que condiciona todo este archivo: un "\n\n" DENTRO de una
// página (salto de párrafo) es indistinguible de un "\n\n" ENTRE páginas. Por eso
// la detección de bordes es fallo-seguro: si la estructura no es uniforme, no
// borra nada. Ver TestConvertDeletesNothingWhenBlankLinesHidePageBoundaries.

func TestConvertRemovesHeaderAndFooterRepeatedOnEveryPage(t *testing.T) {
	t.Parallel()

	content := "INFORME ANUAL\ncuerpo del primer bloque\nCONFIDENCIAL\n\n" +
		"INFORME ANUAL\ncuerpo del segundo bloque\nCONFIDENCIAL"

	want := "cuerpo del primer bloque\n\ncuerpo del segundo bloque"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertRemovesRepeatedHeaderAroundWrappedBodyText(t *testing.T) {
	t.Parallel()

	// Cuerpo real: partido en renglones por PDFium. El header y el pie son la
	// primera y la última línea, no párrafos.
	page := "INFORME ANUAL\nEl scrum es un marco iterativo e incremental para\n" +
		"desarrollar productos de forma continua.\nCONFIDENCIAL"

	content := page + "\n\n" + page

	want := "El scrum es un marco iterativo e incremental para desarrollar productos de forma continua.\n\n" +
		"El scrum es un marco iterativo e incremental para desarrollar productos de forma continua."

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertKeepsHeaderThatAppearsOnlyOnce(t *testing.T) {
	t.Parallel()

	// Una sola aparición no es un header: puede ser el título real del documento,
	// y borrarlo perdería contenido.
	content := "INFORME ANUAL\ncuerpo del unico bloque"

	want := "INFORME ANUAL cuerpo del unico bloque"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertKeepsRepeatedTextThatIsNotOnAPageEdge(t *testing.T) {
	t.Parallel()

	// "parrafo repetido" aparece dos veces pero en el medio de ambas páginas:
	// es contenido, no un header, y no debe tocarse.
	content := "primera pagina empieza\nparrafo repetido\nprimera pagina termina\n\n" +
		"segunda pagina empieza\nparrafo repetido\nsegunda pagina termina"

	want := "primera pagina empieza parrafo repetido primera pagina termina\n\n" +
		"segunda pagina empieza parrafo repetido segunda pagina termina"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertKeepsANearMissOnAPageEdge(t *testing.T) {
	t.Parallel()

	// Pies con número de página variable no se repiten textualmente, así que se
	// conservan. Es el comportamiento conservador buscado: dejar un pie de más
	// es cosmético, borrar contenido es un error visible.
	content := "bloque uno\ncontenido\nPágina 1\n\nbloque dos\ncontenido\nPágina 2"

	want := "bloque uno contenido Página 1\n\nbloque dos contenido Página 2"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertRemovesRepeatedHeaderWhenItIsNotAllCaps(t *testing.T) {
	t.Parallel()

	content := "Informe Anual 2024\nprimer bloque\nPagina 1\n\n" +
		"Informe Anual 2024\nsegundo bloque\nPagina 1"

	want := "primer bloque\n\nsegundo bloque"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

// TestConvertDeletesNothingWhenBlankLinesHidePageBoundaries es el test de
// seguridad de esta heurística. Un "\n\n" interno de la página es indistinguible
// de un "\n\n" entre páginas, así que los "bordes" de cada segmento son en
// realidad texto a mitad del documento.
//
// Con un umbral de repeticiones la heurística borra contenido real: aquí
// "desarrollar productos de forma continua." queda como último renglón de dos
// segmentos y "Se aplica..." como primero de otros dos, así que ambos parecen
// repetirse. Perder texto es inaceptable, por eso el umbral exige que el renglón
// sea borde en TODOS los segmentos: acá no lo es y no se borra nada.
func TestConvertDeletesNothingWhenBlankLinesHidePageBoundaries(t *testing.T) {
	t.Parallel()

	page := "INFORME ANUAL\nEl scrum es un marco iterativo e incremental para\n" +
		"desarrollar productos de forma continua.\n\n" +
		"Se aplica en equipos de hasta diez personas.\nCONFIDENCIAL"

	content := page + "\n\n" + page

	got := Convert(content)

	for _, lost := range []string{"desarrollar productos", "Se aplica en equipos", "INFORME ANUAL", "CONFIDENCIAL"} {
		if !strings.Contains(got, lost) {
			t.Errorf("Convert() = %q, dropped %q", got, lost)
		}
	}
}

// TestConvertKeepsBlankLineParagraphStructure comprueba que, aunque la detección
// de headers se abstenga, los saltos de párrafo siguen siendo párrafos: los
// renglones vacíos se conservan al partir la página.
func TestConvertKeepsBlankLineParagraphStructure(t *testing.T) {
	t.Parallel()

	content := "pagina uno\nprimero\n\nsegundo\n\npagina dos\ntercero"

	want := "pagina uno primero\n\nsegundo\n\npagina dos tercero"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertKeepsAPageThatIsOnlyOneLine(t *testing.T) {
	t.Parallel()

	// Una página de un solo renglón no tiene cuerpo del que quitar nada.
	content := "solo una linea\n\notra pagina con\nmas cuerpo"

	want := "solo una linea\n\notra pagina con mas cuerpo"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

// TestConvertKeepsRepeatedEdgeTextThatAlsoAppearsInsideAPage es la protección
// contra la versión agresiva de la heurística, que borraría cualquier renglón
// repetido de la página y no sólo los bordes. Una empresa que menciona su propio
// nombre en medio del texto no puede perder esa mención.
func TestConvertKeepsRepeatedEdgeTextThatAlsoAppearsInsideAPage(t *testing.T) {
	t.Parallel()

	content := "EMPRESA S.A.\nel informe menciona a EMPRESA S.A. en el cuerpo\nCONFIDENCIAL\n\n" +
		"EMPRESA S.A.\nel anexo repite EMPRESA S.A. al final\nCONFIDENCIAL"

	want := "el informe menciona a EMPRESA S.A. en el cuerpo\n\n" +
		"el anexo repite EMPRESA S.A. al final"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

// TestConvertCountsASegmentOnlyOnceWhenBothEdgesMatch blinda el conteo por
// segmento: si el header y el pie de una página fueran el mismo texto, contar los
// bordes por separado lo haría parecer repetido en dos segmentos distintos y lo
// borraría de páginas donde sí es contenido.
func TestConvertCountsASegmentOnlyOnceWhenBothEdgesMatch(t *testing.T) {
	t.Parallel()

	content := "X\ncuerpo de la primera pagina\nX\n\nX\ncuerpo de la segunda pagina\nX"

	want := "cuerpo de la primera pagina\n\ncuerpo de la segunda pagina"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

// TestConvertDoesNotTreatASingleLinePageAsHavingEdges cubre el otro borde del
// conteo: una página de un solo renglón no aporta bordes. Si contara, bastaría
// con que ese renglón coincidiera con el borde de otra página para eliminarlo,
// y el texto se perdería.
func TestConvertDoesNotTreatASingleLinePageAsHavingEdges(t *testing.T) {
	t.Parallel()

	content := "X\n\nX\ncuerpo de la segunda pagina"

	// "X" se conserva y además es un título por ser una sola letra mayúscula;
	// lo que este test verifica es que no se borre.
	want := "## X\n\nX cuerpo de la segunda pagina"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

// TestConvertMatchesRepeatedEdgesDespiteIndentation fija que los bordes se
// comparan ya recortados. PDFium puede emitir el header indentado o con espacios
// finales, y sin TrimSpace el mismo header dejaría de coincidir y no se quitaría.
func TestConvertMatchesRepeatedEdgesDespiteIndentation(t *testing.T) {
	t.Parallel()

	content := "  EMPRESA S.A.  \ncuerpo de la primera pagina\nCONFIDENCIAL\n\n" +
		"EMPRESA S.A.\ncuerpo de la segunda pagina\nCONFIDENCIAL"

	want := "cuerpo de la primera pagina\n\ncuerpo de la segunda pagina"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}
