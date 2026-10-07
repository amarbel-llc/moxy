package config

import (
	"strings"
	"testing"
)

// A restriction that is written with the wrong type must be an error. Decoding
// it to "unset" would turn a typo into "everything is allowed".
func TestParseRejectsMistypedRestrictions(t *testing.T) {
	for _, src := range []string{
		`allow-tools = "grit.status"`,
		`allow-tools = ["grit", 1]`,
		`allow-tools = true`,
		"[allow-tools]\ngrit = true\n",
		`disable-moxins = "chix"`,
		`disable-moxins = ["chix", 2]`,
		`disable-servers = "dodder"`,
		`builtin-native = "false"`,
		`builtin-native = 0`,
		`resolved-moxins = "none"`,
		`resolved-moxins = ["grit"]`,
		// A non-string entry inside the resolved-moxins table is dropped by
		// the decoder without trace. That is not listed here because it
		// fails closed: a parent record with one moxin fewer admits less.
	} {
		_, err := ParseLiteral([]byte(src))
		if err == nil {
			t.Errorf("%q: expected an error, got none", src)
			continue
		}
		key, _, _ := strings.Cut(strings.TrimPrefix(src, "["), " ")
		key = strings.TrimSuffix(strings.SplitN(key, "]", 2)[0], "]")
		if !strings.Contains(err.Error(), key) {
			t.Errorf("%q: error %q does not name %s", src, err, key)
		}
	}
}

func TestParseAcceptsWellTypedRestrictions(t *testing.T) {
	src := `
allow-tools = ["grit", "folio.read"]
disable-moxins = ["chix"]
disable-servers = ["dodder"]
builtin-native = false

[resolved-moxins]
grit = "/a/grit"
`
	cfg, err := ParseLiteral([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowTools) != 2 || cfg.ResolvedMoxins["grit"] != "/a/grit" {
		t.Errorf("unexpected decode: %+v", cfg)
	}
}

// Keys moxy does not know stay tolerated, as they always were.
func TestParseStillToleratesUnknownKeys(t *testing.T) {
	src := "some-future-key = 1\n[[servers]]\nname = \"s\"\ncommand = \"s\"\nargs = [\"x\"]\n"
	if _, err := ParseLiteral([]byte(src)); err != nil {
		t.Errorf("unknown keys must not be an error: %v", err)
	}
}
