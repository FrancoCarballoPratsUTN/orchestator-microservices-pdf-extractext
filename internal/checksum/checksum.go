package checksum

import (
	"crypto/sha256"
	"encoding/hex"
)

func Of(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
