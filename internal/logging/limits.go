package logging

// DefaultMaxValueLength es el límite por defecto para un valor de log.
const DefaultMaxValueLength = 256

// DefaultMaxStackLength es el límite por defecto para un stack trace.
const DefaultMaxStackLength = 4096

// Limits define los topes aplicados a los valores y stacks de los logs.
type Limits struct {
	MaxValueLength int
	MaxStackLength int
}

// ValueLength devuelve el tope efectivo para un valor de log.
func (l Limits) ValueLength() int {
	if l.MaxValueLength > 0 {
		return l.MaxValueLength
	}
	return DefaultMaxValueLength
}

// StackLength devuelve el tope efectivo para un stack trace.
func (l Limits) StackLength() int {
	if l.MaxStackLength > 0 {
		return l.MaxStackLength
	}
	return DefaultMaxStackLength
}
