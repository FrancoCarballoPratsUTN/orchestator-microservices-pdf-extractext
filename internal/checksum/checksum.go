package checksum

import (
	"crypto/sha256"
	"encoding/hex"
)

func Of(text string) string {
	return OfBytes([]byte(text))
}

// OfBytes hashes raw bytes. PDFs are binary, so the dedup identity must be over
// the exact uploaded bytes, not a string reinterpretation of them.
func OfBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
