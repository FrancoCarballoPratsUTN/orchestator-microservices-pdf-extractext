//go:build golden

package markdown_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// mustGlob es el helper de listado compartido por los tests de golden. Vive acá y
// no en golden_test.go porque ese archivo se compila siempre y no debe arrastrar
// nada del flag golden.
func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()

	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("globbing %s: %v", pattern, err)
	}
	return matches
}

func jsonUnmarshal(raw []byte, target any) error {
	return json.Unmarshal(raw, target)
}
