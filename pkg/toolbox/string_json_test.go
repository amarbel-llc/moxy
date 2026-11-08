package toolbox

import (
	"encoding/json"
	"strings"
	"testing"
)

// moxy#506: `string split --json` and `string match --json` print one JSON
// value per input line, where the structure carries what text loses: empty
// pieces, groups that did not take part, and where a match is. Keys are in a
// fixed order, so the output is deterministic.

func TestStringJSONOutput(t *testing.T) {
	for _, tc := range []struct{ command, stdin, want string }{
		// split: one array per input, empty pieces kept
		{"string split --json ,", "a,b,,c\n", `["a","b","","c"]` + "\n"},
		{"string split --json -n ,", "a,b,,c\n", `["a","b","c"]` + "\n"},
		{"string split --json -m 1 ,", "a,b,c\nx\n", `["a","b,c"]` + "\n" + `["x"]` + "\n"},
		{"string split --json -m 1 -r ,", "a,b,c\n", `["a,b","c"]` + "\n"},
		{"string split --json '' abc", "", `["a","b","c"]` + "\n"},
		{"string split --json ,", "é,\"q\"\\<&>\n", `["é","\"q\"\\<&>"]` + "\n"},
		{"string split --json ,", "a\x00b,c\n", `["a\u0000b","c"]` + "\n"},
		// match -r: one array of matches per input; offsets count code points
		{
			"string match --json -r 'b(.)'", "abc\nxyz\n",
			`[{"start":1,"end":3,"match":"bc","groups":[{"start":2,"end":3,"value":"c"}]}]` + "\n" + `[]` + "\n",
		},
		{
			"string match --json -r -a '(\\d)(x)?'", "1x2\n",
			`[{"start":0,"end":2,"match":"1x","groups":[{"start":0,"end":1,"value":"1"},{"start":1,"end":2,"value":"x"}]},` +
				`{"start":2,"end":3,"match":"2","groups":[{"start":2,"end":3,"value":"2"},null]}]` + "\n",
		},
		{"string match --json -r a", "éa\n", `[{"start":1,"end":2,"match":"a","groups":[]}]` + "\n"},
		{
			"string match --json -r -a ''", "éa\n",
			`[{"start":0,"end":0,"match":"","groups":[]},{"start":1,"end":1,"match":"","groups":[]},{"start":2,"end":2,"match":"","groups":[]}]` + "\n",
		},
		// match with a glob: the whole string, or nothing
		{
			"string match --json 'a*'", "abc\nbca\n",
			`[{"start":0,"end":3,"match":"abc","groups":[]}]` + "\n" + `[]` + "\n",
		},
		{"string match --json 'é?'", "éa\n", `[{"start":0,"end":2,"match":"éa","groups":[]}]` + "\n"},
	} {
		got, err := run(t, t.TempDir(), tc.command, tc.stdin, allowAll)
		if err != nil {
			t.Errorf("%s on %q: %v", tc.command, tc.stdin, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s on %q:\n got %q\nwant %q", tc.command, tc.stdin, got, tc.want)
		}
	}
}

// Every output line is valid JSON, whatever the input.
func TestStringJSONLinesAreValidJSON(t *testing.T) {
	stdin := "plain\n\ntab\there\n\"q\" \\ <&>\nnul\x00byte\né世界 \U0001F600\n"
	for _, cmd := range []string{
		"string split --json ' '",
		"string split --json ''",
		"string match --json -r -a '.'",
		"string match --json -r -a '(\\w)(\\W)?'",
		"string match --json '*'",
	} {
		got, err := run(t, t.TempDir(), cmd, stdin, allowAll)
		if err != nil {
			t.Errorf("%s: %v", cmd, err)
			continue
		}
		lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
		if len(lines) != 6 {
			t.Errorf("%s: %d lines, want one per input line (6)", cmd, len(lines))
		}
		for _, l := range lines {
			if !json.Valid([]byte(l)) {
				t.Errorf("%s: %q is not JSON", cmd, l)
			}
		}
	}
}

func TestStringJSONRefusesInvalidUTF8(t *testing.T) {
	for _, cmd := range []string{"string split --json ,", "string match --json -r a"} {
		_, err := run(t, t.TempDir(), cmd, "a,\xff\n", allowAll)
		if err == nil || !strings.Contains(err.Error(), "UTF-8") {
			t.Errorf("%s: err = %v, want an invalid UTF-8 error: JSON cannot hold arbitrary bytes", cmd, err)
		}
	}
}

func TestStringJSONFlagsAreChecked(t *testing.T) {
	for _, cmd := range []string{
		"string match --json -v x",
		"string match --json -e x",
		"string match --json -r -g '(x)'",
		"string length --json",
		"string upper --json",
	} {
		if _, err := Parse(cmd); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", cmd)
		}
	}
	// -a and -r combine with --json
	if _, err := Parse("string match --json -r -a 'x'"); err != nil {
		t.Errorf("Parse(match --json -r -a): %v", err)
	}
}

func TestStringJSONBoundsItsOutput(t *testing.T) {
	// every character becomes an array of its own: the output outgrows the cap
	stdin := strings.Repeat("ab", 1<<20) + "\n"
	if _, err := run(t, t.TempDir(), "string match --json -r -a ''", stdin, allowAll); err == nil {
		t.Error("a JSON output past the cap was produced")
	}
}
