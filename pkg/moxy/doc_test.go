package moxy

import "testing"

func TestVersionDefaultsToDevWhenUninjected(t *testing.T) {
	// A plain `go test` injects no ldflag; the library must still answer.
	if got := Version(); got == "" {
		t.Fatalf("Version() = %q, want a non-empty value", got)
	}
}
