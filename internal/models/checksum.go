package models

// Checksum identifica un documento por el SHA-256 de los bytes de su PDF. Es la
// clave de dedup contra Persistence: mismos bytes ⇒ mismo checksum ⇒ mismo
// registro.
type Checksum string

// String devuelve el checksum como texto.
func (c Checksum) String() string { return string(c) }
