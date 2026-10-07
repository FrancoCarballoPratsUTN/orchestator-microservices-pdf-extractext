package markdown_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"validationmicroservices-pdf-extractext/internal/markdown"
)

// Estos tests usan salidas REALES del MS Extract sobre los PDFs que ese servicio
// usa para sus pruebas de carga (Conversor/tests/stress/pdfs). Los fixtures se
// capturaron con POST /extract contra la versión del contrato actual
// ({content, page_count}) y quedan en testdata/*.extract.json.
//
// Son la red de seguridad del heurístico: los tests de convert_test.go y
// repeated_test.go usan entradas escritas a mano, que son pequeñas y
// están construidas para el caso que quieren. Estos PDFs traen las rarezas que
// nadie escribe a mano: guiones de des-hifenización pegados a fin de línea, pies
// de página con número variable, encabezados que se repiten sólo en algunas
// páginas y títulos que parecen mayúsculas sin serlo.
//
// Un golden cambia de forma deliberada cuando cambia el Extract. Cuando eso pase,
// revisar el diff del .md es parte del trabajo: un cambio grande y no revisado es
// la señal de que el heuristic se rompió.

// extractFixture es la respuesta real del MS Extract.
type extractFixture struct {
	Content   string `json:"content"`
	PageCount int    `json:"page_count"`
}

// loadFixture lee un .extract.json de testdata.
func loadFixture(t *testing.T, name string) extractFixture {
	t.Helper()

	path := filepath.Join("testdata", name+".extract.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}

	var fixture extractFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parsing fixture %s: %v", path, err)
	}
	if fixture.Content == "" {
		t.Fatalf("fixture %s has empty content", path)
	}
	return fixture
}

// goldenPath es el .md esperado para un fixture.
func goldenPath(name string) string {
	return filepath.Join("testdata", name+".md")
}

// TestConvertAgainstRealExtractOutput compara la conversión completa contra el
// golden de cada PDF real.
func TestConvertAgainstRealExtractOutput(t *testing.T) {
	t.Parallel()

	for _, name := range goldenFixtures(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := loadFixture(t, name)
			got := markdown.Convert(fixture.Content)

			expected, err := os.ReadFile(goldenPath(name))
			if err != nil {
				if os.IsNotExist(err) {
					t.Fatalf("missing golden %s: run `go test ./internal/markdown -update-goldens`", goldenPath(name))
				}
				t.Fatalf("reading golden: %v", err)
			}

			if got != string(expected) {
				t.Errorf("Convert() does not match the golden\n--- got (%d bytes)\n%s\n--- want (%d bytes)\n%s\n--- first difference at byte %d",
					len(got), excerpt(got), len(expected), excerpt(string(expected)), firstDifference(got, string(expected)))
			}
		})
	}
}

// TestConvertIsDeterministicOnRealOutput es el invariante que sostiene el checksum
// sobre PDFs de verdad: la misma entrada tiene que producir el mismo string, siempre.
//
// No se prueba la idempotencia a propósito: el escapado no puede serlo, porque
// convertir "a * b" da "a \* b" y convertir eso de nuevo da "a \\\* b". Eso sólo
// importaría si el markdown volviera a entrar por Extract, y no es el contrato: la
// entrada de Convert es siempre la salida cruda del MS Extract.
func TestConvertIsDeterministicOnRealOutput(t *testing.T) {
	t.Parallel()

	for _, name := range goldenFixtures(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := loadFixture(t, name)

			first := markdown.Convert(fixture.Content)
			for i := 0; i < 20; i++ {
				if got := markdown.Convert(fixture.Content); got != first {
					t.Fatalf("Convert() is not deterministic on run %d: first difference at byte %d", i, firstDifference(first, got))
				}
			}
		})
	}
}

// TestConvertNeverReturnsEmptyForRealPDFs es el invariante del 422: si un PDF real
// devolviera markdown vacío, el servicio respondería 422 sobre un documento que sí
// tiene texto.
func TestConvertNeverReturnsEmptyForRealPDFs(t *testing.T) {
	t.Parallel()

	for _, name := range goldenFixtures(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := loadFixture(t, name)

			if got := markdown.Convert(fixture.Content); got == "" {
				t.Errorf("Convert() = %q, want non-empty for a real PDF with %d pages", got, fixture.PageCount)
			}
		})
	}
}

// TestConvertNeverLosesWordsFromRealPDFs detecta pérdida de contenido. Cuenta las
// palabras significativas del texto crudo y del markdown y exige que no falte
// ninguna.
//
// Es una red ancha a propósito, no una verificación exacta: el escapado agrega
// caracteres y la detección de títulos agrega "##", así que el markdown tiene más
// palabras. Lo que no puede pasar es que el markdown tenga menos.
func TestConvertNeverLosesWordsFromRealPDFs(t *testing.T) {
	t.Parallel()

	for _, name := range goldenFixtures(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := loadFixture(t, name)
			markdownText := markdown.Convert(fixture.Content)

			before := countWords(fixture.Content)
			after := countWords(markdownText)

			if after < before {
				t.Errorf("Convert() lost words: %d in the raw text, %d in the markdown (lost %d)",
					before, after, before-after)
			}
		})
	}
}

// goldenFixtures lista los fixtures .extract.json presentes en testdata.
func goldenFixtures(t *testing.T) []string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join("testdata", "*.extract.json"))
	if err != nil {
		t.Fatalf("globbing testdata: %v", err)
	}

	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, filepath.Base(match[:len(match)-len(".extract.json")]))
	}
	if len(names) == 0 {
		t.Fatal("no fixtures in testdata")
	}
	return names
}

// countWords cuenta palabras de al menos dos letras.
//
// Usa unicode.IsLetter a propósito y no un rango ASCII. El primer intento de este
// conteo trataba "todo rune > 127" como letra y contaba los espacios duros
// (U+00A0) que PDFium deja entre palabras: el Scrum Guide real trae 58, y el
// markdown los quita con TrimSpace. Eso reportaba 29 palabras perdidas que nunca
// se perdieron. IsLetter distingue una letra de un espacio que sólo parece letra.
func countWords(text string) int {
	count, word := 0, 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			word++
			continue
		}
		if word >= 2 {
			count++
		}
		word = 0
	}
	if word >= 2 {
		count++
	}
	return count
}

// firstDifference devuelve el índice del primer byte distinto entre dos strings.
func firstDifference(got, want string) int {
	limit := len(got)
	if len(want) < limit {
		limit = len(want)
	}
	for i := 0; i < limit; i++ {
		if got[i] != want[i] {
			return i
		}
	}
	return limit
}

// excerpt recorta una salida larga para que el mensaje de fallo siga siendo legible.
func excerpt(text string) string {
	const limit = 600

	if len(text) <= limit {
		return text
	}
	return text[:limit] + "\n... [truncado]"
}

// TestConvertNormalizesUnicodeSpaces cubre el NBSP interno.
//
// Regresión: el golden de Essential Kanban traía seis espacios duros dentro de
// las líneas, en cosas como "(ver Fig 14)" y "Large and multiple services".
// TrimSpace limpiaba los bordes de cada renglón pero no el interior, así que
// llegaban al markdown que el cliente persiste. El defecto era invisible en una
// revisión visual: U+00A0 se ve igual que un espacio.
//
// Lo encontró el spike de k6, no un test: la métrica de artefactos marcaba 75 %
// de las respuestas, es decir exactamente uno de cada cuatro documentos del
// corpus. Por eso el caso va también contra un PDF real.
func TestConvertNormalizesUnicodeSpaces(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"non-breaking space inside a line", "ver Fig 14 ahora", "ver Fig 14 ahora"},
		{"several non-breaking spaces", "a  b", "a b"},
		{"mixed with ascii spaces", "a  b", "a b"},
		{"en quad", "a b", "a b"},
		{"narrow no-break space", "a b", "a b"},
		{"ideographic space", "a　b", "a b"},
		{"tabs are still collapsed", "a\t\tb", "a b"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := markdown.Convert(tc.input); got != tc.want {
				t.Errorf("Convert(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestConvertOnRealCorpusHasNoUnicodeSpaces es la versión sobre datos reales, para
// que el contrato no dependa de que alguien recuerde escribir el caso unitario.
func TestConvertOnRealCorpusHasNoUnicodeSpaces(t *testing.T) {
	t.Parallel()

	for _, name := range goldenFixtures(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			md := markdown.Convert(loadFixture(t, name).Content)
			for _, r := range md {
				if unicode.IsSpace(r) && r != ' ' && r != '\n' {
					t.Fatalf("markdown contains space rune %U; only ' ' and '\\n' may remain", r)
				}
			}
			if strings.ContainsRune(md, '\r') {
				t.Error("markdown contains a carriage return from PDFium's line endings")
			}
		})
	}
}
