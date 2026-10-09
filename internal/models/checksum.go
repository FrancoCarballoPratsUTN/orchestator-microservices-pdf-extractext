package models

// Checksum identifica un documento por el SHA-256 de los bytes de su PDF. Es la
// clave de dedup contra Persistence: mismos bytes ⇒ mismo checksum ⇒ mismo
// registro. Antes de la Fase 8 era el SHA-256 del texto extraído.
type Checksum string

func (c Checksum) String() string { return string(c) }
