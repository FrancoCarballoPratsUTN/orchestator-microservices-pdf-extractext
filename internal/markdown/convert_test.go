package markdown

import (
	"strings"
	"testing"
)

func TestConvertReturnsEmptyStringForEmptyContent(t *testing.T) {
	t.Parallel()

	if got := Convert(""); got != "" {
		t.Errorf("Convert(%q) = %q, want empty", "", got)
	}
}

func TestConvertReturnsEmptyStringForWhitespaceOnlyContent(t *testing.T) {
	t.Parallel()

	if got := Convert("   \n\n\t \n  "); got != "" {
		t.Errorf("Convert(whitespace) = %q, want empty", got)
	}
}

func TestConvertUnwrapsALineWrappedParagraphIntoOneLine(t *testing.T) {
	t.Parallel()

	// así es como PDFium entrega el texto: cortado cada ~80 columnas.
	content := "El scrum es un marco de trabajo iterativo e incremental para\n" +
		"desarrollar y validar productos de forma continua a lo largo del\n" +
		"ciclo de vida del proyecto.\n\n"

	want := "El scrum es un marco de trabajo iterativo e incremental para desarrollar y validar productos de forma continua a lo largo del ciclo de vida del proyecto."

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertKeepsBlankLineAsParagraphSeparator(t *testing.T) {
	t.Parallel()

	content := "primer parrafo en una sola linea\n\nsegundo parrafo en una sola linea"

	want := "primer parrafo en una sola linea\n\nsegundo parrafo en una sola linea"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertJoinsWordSplitAcrossLinesWithoutTheHyphen(t *testing.T) {
	t.Parallel()

	content := "El-doctor que sabe comunicar su trabajo con los equipos\n" +
		"técnicos es aquél que ha aprendi-\n" +
		"do a fragmentar problemas grandes."

	want := "El-doctor que sabe comunicar su trabajo con los equipos técnicos es aquél que ha aprendido a fragmentar problemas grandes."

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertDoesNotJoinHyphenFollowedByUppercase(t *testing.T) {
	t.Parallel()

	// " -" seguido de mayúscula es una enumeración o un título, no una
	// palabra partida: unirlas sería perder el salto de página.
	content := "capitulo 3 -\nSCRUM EN LA PRÁCTICA\n"

	want := "capitulo 3 - SCRUM EN LA PRÁCTICA"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertDoesNotTreatDoubleHyphenAsSplitWord(t *testing.T) {
	t.Parallel()

	content := "el marcador se escribe así --\ncomo en este texto"

	want := "el marcador se escribe así -- como en este texto"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

// TestConvertSeparatesWindowsLineEndingPagesLikeUnixOnes documenta una decisión,
// no una normalización: NO hay ReplaceAll de \r\n, pero el resultado es el mismo
// porque un \r\n\r\n cae como párrafo vacío y los párrafos también se unen con
// \n\n. Si algún día se agrega la normalización, este test avisa.
func TestConvertSeparatesWindowsLineEndingPagesLikeUnixOnes(t *testing.T) {
	t.Parallel()

	crlf := Convert("contenido de la pagina uno\r\n\r\ncontenido de la pagina dos")
	unix := Convert("contenido de la pagina uno\n\ncontenido de la pagina dos")

	if crlf != unix {
		t.Errorf("CRLF = %q, want same as LF = %q", crlf, unix)
	}
	if unix != "contenido de la pagina uno\n\ncontenido de la pagina dos" {
		t.Errorf("output = %q, want pages separated by blank line", unix)
	}
}

func TestConvertUnwrapsParagraphsWithWindowsLineEndings(t *testing.T) {
	t.Parallel()

	content := "primera linea del parrafo\r\nsegunda linea del parrafo"

	want := "primera linea del parrafo segunda linea del parrafo"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

// TestConvertDropsCarriageReturnsLeftInsideLines blinda el invariante que hace
// innecesario normalizar \r\n: TrimSpace por línea tiene que eliminar el \r.
func TestConvertDropsCarriageReturnsLeftInsideLines(t *testing.T) {
	t.Parallel()

	content := "primera linea\r\nsegunda linea"

	if got := Convert(content); strings.ContainsAny(got, "\r\t") {
		t.Errorf("Convert() = %q, want no carriage return or tab", got)
	}
}

func TestConvertSplitsPagesAndRejoinsThemWithABlankLine(t *testing.T) {
	t.Parallel()

	// El Extract une páginas con \n\n, así que \n\n es borde de página.
	content := "contenido de la pagina uno\n\ncontenido de la pagina dos"

	want := "contenido de la pagina uno\n\ncontenido de la pagina dos"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertCollapsesRunsOfSpacesAndTabs(t *testing.T) {
	t.Parallel()

	content := "palabra    con      espacios\ty  tabuladores"

	want := "palabra con espacios y tabuladores"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertDropsPagesThatAreOnlyWhitespace(t *testing.T) {
	t.Parallel()

	content := "contenido real\n\n   \n\n   \t  \n\nmas contenido real"

	want := "contenido real\n\nmas contenido real"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

// TestConvertIsDeterministic protege el invariante del sistema: el checksum es
// el SHA-256 de esta salida, así que dos corridas con la misma entrada tienen
// que dar exactamente el mismo string.
func TestConvertIsDeterministic(t *testing.T) {
	t.Parallel()

	content := "PRIMERA PAGINA\n\nlinea uno de la segunda\nlinea dos\n\nTERCERA PAGINA"

	first := Convert(content)
	for i := 0; i < 50; i++ {
		if got := Convert(content); got != first {
			t.Fatalf("Convert() is not deterministic: run %d = %q, first = %q", i, got, first)
		}
	}
}

// TestConvertNeverEmptiesNonEmptyContent es el invariante que sostiene el 422
// del servicio: si Convert devolviera "" para contenido con texto, el checksum
// sería SHA-256("") y volveríamos a la colisión de IDs que el 422 evita.
func TestConvertNeverEmptiesNonEmptyContent(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"una sola palabra",
		"solo espacios alrededor",
		"guion al final de la unica linea -",
		"a",
	}

	for _, input := range inputs {
		if got := Convert(input); got == "" {
			t.Errorf("Convert(%q) = %q, want non-empty", input, got)
		}
	}
}
