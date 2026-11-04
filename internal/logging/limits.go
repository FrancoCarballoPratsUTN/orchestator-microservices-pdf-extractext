package logging

const (
	DefaultMaxValueLength = 256
	DefaultMaxStackLength = 4096
)

type Limits struct {
	MaxValueLength int
	MaxStackLength int
}

func (l Limits) ValueLength() int {
	if l.MaxValueLength > 0 {
		return l.MaxValueLength
	}
	return DefaultMaxValueLength
}

func (l Limits) StackLength() int {
	if l.MaxStackLength > 0 {
		return l.MaxStackLength
	}
	return DefaultMaxStackLength
}
