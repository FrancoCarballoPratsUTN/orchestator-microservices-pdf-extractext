package checksum

import (
	"crypto/sha256"
	"encoding/hex"
)

// Of devuelve el SHA-256 hexadecimal del texto recibido.
func Of(text string) string {
	return OfBytes([]byte(text))
}

// OfBytes calcula el SHA-256 de los bytes crudos. Un PDF es binario, así que la
// identidad de dedup debe calcularse sobre los bytes exactos subidos, no sobre
// una reinterpretación textual de ellos.
func OfBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
