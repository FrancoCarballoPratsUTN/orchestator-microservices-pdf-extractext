//go:build golden

package markdown_test

import (
	"os"
	"path/filepath"
	"testing"

	"validationmicroservices-pdf-extractext/internal/markdown"
)

// TestUpdateGoldens regenera los .md de testdata desde los .extract.json.
//
// Se corre a mano y sólo cuando el cambio en la conversión es intencional:
//
//	go test -tags golden ./internal/markdown -run TestUpdateGoldens
//
// El diff de los .md resultante es lo que hay que revisar. Regenerar goldens
// "para que pase" sin leer el diff es la forma de perder texto sin enterarse: por
// eso los tests normales incluyen comprobaciones de pérdida de palabras que no
// dependen del golden (TestConvertNeverLosesWordsFromRealPDFs).
func TestUpdateGoldens(t *testing.T) {
	for _, match := range mustGlob(t, filepath.Join("testdata", "*.extract.json")) {
		name := filepath.Base(match[:len(match)-len(".extract.json")])

		raw, err := os.ReadFile(match)
		if err != nil {
			t.Fatalf("reading %s: %v", match, err)
		}

		var fixture extractFixture
		if err := jsonUnmarshal(raw, &fixture); err != nil {
			t.Fatalf("parsing %s: %v", match, err)
		}

		path := filepath.Join("testdata", name+".md")
		if err := os.WriteFile(path, []byte(markdown.Convert(fixture.Content)), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		t.Logf("wrote %s", path)
	}
}
