package logging

import "testing"

func TestLimitsUseSafeDefaultsWhenUnset(t *testing.T) {
	limits := Limits{}
	if got := limits.ValueLength(); got != DefaultMaxValueLength {
		t.Errorf("ValueLength() = %d, want %d", got, DefaultMaxValueLength)
	}
	if got := limits.StackLength(); got != DefaultMaxStackLength {
		t.Errorf("StackLength() = %d, want %d", got, DefaultMaxStackLength)
	}
}

func TestLimitsPreserveConfiguredValues(t *testing.T) {
	limits := Limits{MaxValueLength: 180, MaxStackLength: 2048}
	if got := limits.ValueLength(); got != 180 {
		t.Errorf("ValueLength() = %d, want 180", got)
	}
	if got := limits.StackLength(); got != 2048 {
		t.Errorf("StackLength() = %d, want 2048", got)
	}
}
