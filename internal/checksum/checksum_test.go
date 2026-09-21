package checksum

import (
	"strings"
	"testing"
)

func TestOfProducesSHA256Hex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "empty text returns SHA-256 of empty input",
			text: "",
			want: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name: "known vector for 'abc'",
			text: "abc",
			want: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		},
		{
			name: "English sentence",
			text: "The quick brown fox jumps over the lazy dog",
			want: "d7a8fbb307d7809469ca9abcb0082e4f8d5651e46d3cdb762d02d0bf37c9e592",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Of(tt.text)

			if got != tt.want {
				t.Fatalf("Of(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestOfIsDeterministicAndLowerCaseHex(t *testing.T) {
	t.Parallel()

	first := Of("mismo texto de prueba")
	second := Of("mismo texto de prueba")

	if first != second {
		t.Fatalf("Of is not deterministic: %q != %q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("Of() returned %d characters, want 64", len(first))
	}
	if first != strings.ToLower(first) {
		t.Fatalf("Of() = %q, want lowercase hex", first)
	}
}

func TestOfChangesWhenTextChanges(t *testing.T) {
	t.Parallel()

	if Of("first version") == Of("second version") {
		t.Fatal("different inputs must produce different checksums")
	}
}
