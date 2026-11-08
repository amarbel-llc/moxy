package toolbox

import (
	"context"
	"strings"
	"testing"
	"time"
)

// moxy#502 (#498 folded in): `string` is one in-process applet spelled like
// fish's builtin, `string <subcommand> [flags] [args] [strings...]`. With no
// string operands it reads its input's lines.

func TestStringSubcommands(t *testing.T) {
	for _, tc := range []struct{ command, stdin, want string }{
		{"string length", "abc\n\nnaïve\n", "3\n0\n5\n"},
		{"string length abc ''", "ignored\n", "3\n0\n"},
		// combining characters are code points of their own
		{"string length", "é\n", "2\n"},
		{"string length", "a\x00b\x01\n", "4\n"},
		{"string upper", "naïve\n", "NAÏVE\n"},
		{"string lower ABC", "", "abc\n"},
		{"string sub -s 2 -l 3", "abcdef\n", "bcd\n"},
		{"string sub -s -2", "abcdef\n", "ef\n"},
		{"string sub -s 2 -e -2", "abcdef\n", "bcde\n"},
		{"string sub -l 10", "ab\n", "ab\n"},
		{"string trim", "  a b \t\n", "a b\n"},
		{"string trim -l", "  a  \n", "a  \n"},
		{"string trim -r -c x", "xxaxx\n", "xxa\n"},
		{"string split ,", "a,b,,c\n", "a\nb\n\nc\n"},
		{"string split -n ,", "a,b,,c\n", "a\nb\nc\n"},
		{"string split -m 1 ,", "a,b,c\n", "a\nb,c\n"},
		{"string split -m 1 -r ,", "a,b,c\n", "a,b\nc\n"},
		{"string split '' abc", "", "a\nb\nc\n"},
		{"string join +", "1\n2\n3\n", "1+2+3\n"},
		{"string join ', ' a b", "", "a, b\n"},
		{"string replace o 0", "foo boo\n", "f0o boo\n"},
		{"string replace -a o 0", "foo boo\n", "f00 b00\n"},
		{"string replace -r '(\\w+)@(\\w+)' '$2 at $1'", "me@host\n", "host at me\n"},
		{"string replace -f x y", "axe\nbee\n", "aye\n"},
		{"string match 'a*'", "abc\nbca\na\n", "abc\na\n"},
		{"string match -v 'a*'", "abc\nbca\n", "bca\n"},
		{"string match -r 'b(.)'", "abc\nxyz\n", "bc\nc\n"},
		{"string match -r -g 'b(.)'", "abc\n", "c\n"},
		{"string match -r -a '[0-9]'", "a1b2\n", "1\n2\n"},
		{"string match -r -e '[0-9]'", "a1b2\nnone\n", "a1b2\n"},
		{"string pad -w 4", "ab\nabcdef\n", "  ab\nabcdef\n"},
		{"string pad -r -w 4 -c . ab", "", "ab..\n"},
		{"string repeat -n 3 ab", "", "ababab\n"},
		{"string repeat -n 3 -m 5 ab", "", "ababa\n"},
		{"string repeat -N -n 2 x", "", "xx"},
		{"string collapse", "  a \t b\n\nc  \n", " a b\n\nc \n"},
		// a pipeline: string stages read the previous stage's output
		{"string split , | string upper | string join -", "a,b\n", "A-B\n"},
		{"string length", "", ""},
	} {
		got, err := run(t, t.TempDir(), tc.command, tc.stdin, allowAll)
		if err != nil {
			t.Errorf("%s: %v", tc.command, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s on %q = %q, want %q", tc.command, tc.stdin, got, tc.want)
		}
	}
}

func TestStringParseErrors(t *testing.T) {
	for _, cmd := range []string{
		"string",
		"string nope",
		"string split",
		"string join",
		"string replace a",
		"string match",
		"string pad -w x",
		"string repeat",
		"string repeat -n -1 x",
		"string sub -s 0",
		"string length --nope",
		"string upper -r",
		"string match -r '('",
	} {
		if _, err := Parse(cmd); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", cmd)
		}
	}
}

func TestStringUsageErrorsNameTheirPage(t *testing.T) {
	_, err := Parse("string trim -x")
	if err == nil || !strings.Contains(err.Error(), "toolbox-string(1)") {
		t.Errorf("err = %v, want a pointer to toolbox-string(1)", err)
	}
}

// string's operands are strings, not paths, like tr's sets: nothing for the
// policy to judge, and nothing is ever opened.
func TestStringOperandsAreNotFiles(t *testing.T) {
	p, err := Parse("string length /etc/passwd | string upper madder://blobs/blake2b256-abc")
	if err != nil {
		t.Fatal(err)
	}
	if files := p.Files(); len(files) != 0 {
		t.Errorf("Files = %q, want none", files)
	}
	got, err := run(t, t.TempDir(), "string length /etc/passwd", "", func(_ context.Context, path string) error {
		t.Errorf("asked about %q", path)
		return nil
	})
	if err != nil || got != "11\n" {
		t.Errorf("got %q, %v", got, err)
	}
}

// RE2: a pattern that backtracks exponentially elsewhere is linear here.
func TestStringRegexDoesNotBacktrack(t *testing.T) {
	input := strings.Repeat("a", 100_000) + "!\n"
	start := time.Now()
	got, err := run(t, t.TempDir(), "string match -r '(a+)+$'", input, allowAll)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("matched %d bytes", len(got))
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestStringBoundsInputAndOutput(t *testing.T) {
	big := strings.Repeat("x", MaxStringBytes+1)
	if _, err := run(t, t.TempDir(), "string length", big, allowAll); err == nil {
		t.Error("input over the cap was accepted")
	}
	if _, err := run(t, t.TempDir(), "string repeat -n 1000000000 abcdefgh", "", allowAll); err == nil {
		t.Error("output over the cap was produced")
	}
	if _, err := run(t, t.TempDir(), "string pad -w 1000000000 x", "", allowAll); err == nil {
		t.Error("a pad over the cap was produced")
	}
}
