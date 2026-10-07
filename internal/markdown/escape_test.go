package markdown

import (
	"strings"
	"testing"
)

func TestConvertPromotesAllCapsParagraphToHeading(t *testing.T) {
	t.Parallel()

	content := "SCRUM EN LA PRÁCTICA\n\nEl scrum es un marco iterativo."

	want := "## SCRUM EN LA PRÁCTICA\n\nEl scrum es un marco iterativo."

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertPromotesAllCapsHeadingWithDigits(t *testing.T) {
	t.Parallel()

	content := "CAPÍTULO 1\n\ntexto normal"

	want := "## CAPÍTULO 1\n\ntexto normal"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

// TestConvertKeepsSentenceWithLowercaseAsBodyText blinda el riesgo principal de
// la heurística: el español y el inglés escriben frases con minúsculas, así que
// cualquier mezcla tiene que seguir siendo párrafo.
func TestConvertKeepsSentenceWithLowercaseAsBodyText(t *testing.T) {
	t.Parallel()

	content := "El SCRUM se define por sprints"

	want := "El SCRUM se define por sprints"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertDoesNotPromoteAllCapsSentenceOverTheWordLimit(t *testing.T) {
	t.Parallel()

	words := make([]string, maxHeadingWords+1)
	for i := range words {
		words[i] = "PALABRA"
	}

	content := strings.Join(words, " ")

	if got := Convert(content); got != content {
		t.Errorf("Convert() = %q, want the sentence unchanged", got)
	}
}

// TestConvertPromotesAllCapsParagraphExactlyAtTheWordLimit fija el borde del
// umbral de palabras: exactamente maxHeadingWords todavía es título.
func TestConvertPromotesAllCapsParagraphExactlyAtTheWordLimit(t *testing.T) {
	t.Parallel()

	words := make([]string, maxHeadingWords)
	for i := range words {
		words[i] = "PALABRA"
	}

	content := strings.Join(words, " ")

	want := headingMarker + content
	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertDoesNotPromoteNumbersOnlyParagraph(t *testing.T) {
	t.Parallel()

	content := "2024\n\ntexto normal"

	want := "2024\n\ntexto normal"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

// TestConvertDoesNotPromoteLongSingleTokenAllCapsParagraph cubre el artefacto que
// el límite de palabras no alcanza: un blob base64 o un ID de seguimiento llega
// como un único token larguísimo en mayúsculas, y "una sola palabra" lo aprobaría
// como título. Por eso existe maxHeadingLength.
func TestConvertDoesNotPromoteLongSingleTokenAllCapsParagraph(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("A", maxHeadingLength+1)

	if got := Convert(long); got != long {
		t.Errorf("Convert() = %q, want the token unchanged", got)
	}
}

// TestConvertPromotesAllCapsParagraphExactlyAtTheLengthLimit fija el borde
// exacto del umbral. La guarda es len > maxHeadingLength, así que un párrafo de
// exactamente maxHeadingLength caracteres todavía es título; usar maxHeadingLength-1
// dejaría el "> " vs ">=" sin verificar.
func TestConvertPromotesAllCapsParagraphExactlyAtTheLengthLimit(t *testing.T) {
	t.Parallel()

	heading := strings.Repeat("A", maxHeadingLength)

	want := headingMarker + heading
	if got := Convert(heading); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertPromotesLineWrappedAllCapsHeading(t *testing.T) {
	t.Parallel()

	// el título llega partido en dos líneas por el corte de línea de PDFium
	content := "PRINCIPIOS\nFUNDAMENTALES\n\ntexto normal"

	want := "## PRINCIPIOS FUNDAMENTALES\n\ntexto normal"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertEscapesMarkdownSyntaxInBodyText(t *testing.T) {
	t.Parallel()

	content := "multiplica 3 * 4 y guarda en snake_case"

	want := `multiplica 3 \* 4 y guarda en snake\_case`

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertEscapesBackticksAndBrackets(t *testing.T) {
	t.Parallel()

	content := "el campo `nombre` y el vector [x, y]"

	want := "el campo \\`nombre\\` y el vector \\[x, y\\]"

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

func TestConvertEscapesBackslashItself(t *testing.T) {
	t.Parallel()

	content := `la ruta es C:\carpeta\archivo`

	want := `la ruta es C:\\carpeta\\archivo`

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

// TestConvertEscapesInsideHeadingButKeepsItsMarker cubre que el "#" de apertura lo
// pone la conversión y no se escapa, mientras que el cuerpo sí.
func TestConvertEscapesInsideHeadingButKeepsItsMarker(t *testing.T) {
	t.Parallel()

	content := "AVISO: NO *HACER* ESTO"

	want := `## AVISO: NO \*HACER\* ESTO`

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}

// TestConvertEscapesOncePerCharacter evita el doble escapado: un "*" tiene que
// producir exactamente un "\*", nunca "\\*". Si el escapado fuera recursivo, el
// checksum dependería de cuántas veces se ejecutara la conversión.
func TestConvertEscapesOncePerCharacter(t *testing.T) {
	t.Parallel()

	content := `un * y otro *`

	want := `un \* y otro \*`

	if got := Convert(content); got != want {
		t.Errorf("Convert() = %q, want %q", got, want)
	}
}
