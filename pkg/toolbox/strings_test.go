package toolbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moxy#184: `strings` prints the printable runs of a binary input, one per
// line, so `strings bin | string match -r PATTERN` checks a build for a
// string without a shell.
func TestStringsPrintsPrintableRuns(t *testing.T) {
	dir := t.TempDir()
	bin := []byte("\x00\x01ELF\x00xlen_sw\x00\xffab\x00NO_CURRENT_EF\ttab\x7f" + strings.Repeat("z", 3))
	if err := os.WriteFile(filepath.Join(dir, "bin"), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ command, want string }{
		{"strings bin", "xlen_sw\nNO_CURRENT_EF\ttab\n"},
		{"strings -n 3 bin", "ELF\nxlen_sw\nNO_CURRENT_EF\ttab\nzzz\n"},
		{"strings --bytes=8 bin", "NO_CURRENT_EF\ttab\n"},
		{"strings bin | string match -re 'xlen|EF'", "xlen_sw\nNO_CURRENT_EF\ttab\n"},
	}
	for _, c := range cases {
		got, err := run(t, dir, c.command, "", allowAll)
		if err != nil {
			t.Fatalf("%s: %v", c.command, err)
		}
		if got != c.want {
			t.Errorf("%s = %q, want %q", c.command, got, c.want)
		}
	}
}

func TestStringsReadsStdinAndRefusesAZeroLength(t *testing.T) {
	got, err := run(t, t.TempDir(), "strings", "\x00hello\x00hi\x00", allowAll)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello\n" {
		t.Errorf("strings of stdin = %q, want %q", got, "hello\n")
	}
	if _, err := Parse("strings -n 0"); err == nil {
		t.Error("strings -n 0 parsed; a zero minimum would print every byte")
	}
}
