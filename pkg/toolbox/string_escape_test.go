package toolbox

import (
	"strings"
	"testing"
)

// moxy#505: `string escape` and `string unescape`, with --style shell (the
// default), url or json.

func TestStringEscapeAndUnescape(t *testing.T) {
	for _, tc := range []struct{ command, stdin, want string }{
		// shell: one single-quoted sh word
		{"string escape", "plain\n", "'plain'\n"},
		{"string escape", "it's $x `y` \"q\" \\\n", `'it'\''s $x ` + "`y`" + ` "q" \'` + "\n"},
		{"string escape -- ''", "", "''\n"},
		{"string escape --style=shell a b", "", "'a'\n'b'\n"},
		{"string unescape", `'it'\''s'` + "\n", "it's\n"},
		{"string unescape", `a'b c'"d e"\ f` + "\n", "ab cd e f\n"},
		{"string unescape", `"x\"y\\z\$"` + "\n", `x"y\z$` + "\n"},
		{"string unescape", "''\n", "\n"},
		// url: percent-encoding of every byte but the unreserved ones
		{"string escape --style=url", "a b/c?d=é-._~\n", "a%20b%2Fc%3Fd%3D%C3%A9-._~\n"},
		{"string escape --style=url", "\xff\x00\n", "%FF%00\n"},
		{"string unescape --style=url", "a%20b%2fc%3Fd%3D%C3%A9+-._~\n", "a b/c?d=é+-._~\n"},
		// json: a string literal's body, or the literal under --quotes
		{"string escape --style=json", "a\tb\x00c\"d\\e\x7f\n", `a\tb\u0000c\"d\\e` + "\x7f\n"},
		{"string escape --style=json", "<&>é世\n", "<&>é世\n"},
		{"string escape --style=json --quotes", "hi\n", `"hi"` + "\n"},
		{"string unescape --style=json", `a\tb\u0000c\"d\\eé\/` + "\n", "a\tb\x00c\"d\\eé/\n"},
		{"string unescape --style=json --quotes", `"hi\n"` + "\n", "hi\n\n"},
	} {
		got, err := run(t, t.TempDir(), tc.command, tc.stdin, allowAll)
		if err != nil {
			t.Errorf("%s on %q: %v", tc.command, tc.stdin, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s on %q = %q, want %q", tc.command, tc.stdin, got, tc.want)
		}
	}
}

func TestStringEscapeRoundTrips(t *testing.T) {
	inputs := []string{
		"plain", "", "it's $x `y` \"q\" \\", "tab\there", "é世界 \U0001F600", "nul\x00byte",
		"%41 +", "a'b", "''", `A "x"`, " leading and trailing ",
	}
	stdin := strings.Join(inputs, "\n") + "\n"
	for _, style := range []string{"shell", "url", "json"} {
		for _, quotes := range []string{"", " --quotes"} {
			if quotes != "" && style != "json" {
				continue
			}
			cmd := "string escape --style=" + style + quotes + " | string unescape --style=" + style + quotes
			got, err := run(t, t.TempDir(), cmd, stdin, allowAll)
			if err != nil {
				t.Errorf("%s: %v", cmd, err)
				continue
			}
			if got != stdin {
				t.Errorf("%s = %q, want %q", cmd, got, stdin)
			}
		}
	}
}

func TestStringUnescapeRefusesBadInputWithAClearError(t *testing.T) {
	for _, tc := range []struct{ command, stdin, wantErr string }{
		{"string unescape --style=url", "%zz\n", "invalid percent escape"},
		{"string unescape --style=url", "abc%4\n", "invalid percent escape"},
		{"string unescape --style=url", "%\n", "invalid percent escape"},
		{"string unescape --style=json", `a\q` + "\n", "json"},
		{"string unescape --style=json", `a"b` + "\n", "json"},
		{"string unescape --style=json", `\u12` + "\n", "json"},
		{"string unescape --style=json --quotes", "no quotes\n", "json"},
		{"string unescape", `'unterminated` + "\n", "unterminated"},
		{"string unescape", `"unterminated` + "\n", "unterminated"},
		{"string unescape", `trailing\` + "\n", "trailing backslash"},
		{"string unescape", "two words\n", "more than one word"},
	} {
		_, err := run(t, t.TempDir(), tc.command, tc.stdin, allowAll)
		if err == nil {
			t.Errorf("%s on %q succeeded, want an error", tc.command, tc.stdin)
			continue
		}
		if !strings.Contains(err.Error(), "string unescape") || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s on %q: err = %q, want it to name string unescape and %q", tc.command, tc.stdin, err, tc.wantErr)
		}
	}
}

func TestStringEscapeJSONRefusesInvalidUTF8(t *testing.T) {
	_, err := run(t, t.TempDir(), "string escape --style=json", "ok\xff\n", allowAll)
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Errorf("err = %v, want an invalid UTF-8 error: a JSON string cannot hold arbitrary bytes", err)
	}
	// the other styles are byte-wise and take any input
	for _, cmd := range []string{"string escape", "string escape --style=url"} {
		if _, err := run(t, t.TempDir(), cmd, "ok\xff\n", allowAll); err != nil {
			t.Errorf("%s on invalid UTF-8: %v", cmd, err)
		}
	}
}

func TestStringEscapeFlagsAreChecked(t *testing.T) {
	for _, cmd := range []string{
		"string escape --style=bogus x",
		"string unescape --style=",
		"string escape --style=url --quotes x",
		"string unescape --quotes x",
	} {
		if _, err := Parse(cmd); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", cmd)
		}
	}
}

func TestStringEscapeBoundsItsOutput(t *testing.T) {
	// every byte of the input becomes three in the url style
	stdin := strings.Repeat("é", MaxStringBytes/4) + "\n"
	if _, err := run(t, t.TempDir(), "string escape --style=url", stdin, allowAll); err == nil {
		t.Error("an escape that would grow past the output cap succeeded")
	}
}
