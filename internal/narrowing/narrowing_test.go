package narrowing

import (
	"reflect"
	"testing"

	"code.linenisgreat.com/moxy/internal/config"
)

func mustParse(t *testing.T, src string) config.Config {
	t.Helper()
	cfg, err := config.ParseLiteral([]byte(src))
	if err != nil {
		t.Fatalf("parsing %q: %v", src, err)
	}
	return cfg
}

func violationLines(vs []Violation) []string {
	lines := make([]string, len(vs))
	for i, v := range vs {
		lines[i] = v.String()
	}
	return lines
}

func TestCheck(t *testing.T) {
	const smith = `
[[servers]]
name = "smith"
command = ["smith", "mcp"]
`
	cases := []struct {
		name   string
		parent string
		child  string
		want   []string
	}{
		{
			name:   "identical configs narrow",
			parent: smith,
			child:  smith,
		},
		{
			name:   "empty child narrows empty parent",
			parent: "",
			child:  "",
		},
		{
			name:   "string and array command forms are the same invocation",
			parent: smith,
			child:  "[[servers]]\nname = \"smith\"\ncommand = \"smith mcp\"\n",
		},
		{
			name:   "child may omit a server when it disables the same-named moxin",
			parent: smith,
			child:  `disable-moxins = ["smith"]`,
		},
		{
			name:   "child omitting a server without disabling its moxin un-shadows it",
			parent: smith,
			child:  "",
			want: []string{
				"disable-moxins: parent server smith shadows the moxin of that name; child neither defines the server nor disables the moxin",
			},
		},
		{
			name:   "child may not add a server",
			parent: "",
			child:  smith,
			want:   []string{"servers.smith: not in parent"},
		},
		{
			name:   "child may not change a command",
			parent: smith,
			child:  "[[servers]]\nname = \"smith\"\ncommand = [\"smith\", \"mcp\", \"--admin\"]\n",
			want:   []string{"servers.smith: command differs from parent"},
		},
		{
			name:   "an argument containing a space is not the same as two arguments",
			parent: "[[servers]]\nname = \"s\"\ncommand = [\"s\", \"a b\"]\n",
			child:  "[[servers]]\nname = \"s\"\ncommand = [\"s\", \"a\", \"b\"]\n",
			want:   []string{"servers.s: command differs from parent"},
		},
		{
			name:   "child may not run a server the parent has disabled",
			parent: "disable-servers = [\"smith\"]\n" + smith,
			child:  smith,
			want:   []string{"servers.smith: disabled in parent"},
		},
		{
			name:   "child may define a parent-disabled server while keeping it disabled",
			parent: "disable-servers = [\"smith\"]\n" + smith,
			child:  "disable-servers = [\"smith\"]\n" + smith,
		},
		{
			name:   "a server the child itself disables is not compared",
			parent: "",
			child:  "disable-servers = [\"smith\"]\n" + smith,
		},
		{
			name:   "child may not wrap a server in a devshell",
			parent: smith,
			child:  smith + "nix-devshell = \".\"\n",
			want:   []string{"servers.smith: nix-devshell differs from parent"},
		},
		{
			name:   "url servers compare url, headers, helper and oauth",
			parent: "[[servers]]\nname = \"h\"\nurl = \"https://a.example/mcp\"\nheaders-helper = \"tok a\"\n[servers.headers]\nAuthorization = \"Bearer $A\"\n[servers.oauth]\nclient-id = \"one\"\n",
			child:  "[[servers]]\nname = \"h\"\nurl = \"https://b.example/mcp\"\nheaders-helper = \"tok b\"\n[servers.headers]\nAuthorization = \"Bearer $B\"\n[servers.oauth]\nclient-id = \"two\"\n",
			want: []string{
				"servers.h: headers differs from parent",
				"servers.h: headers-helper differs from parent",
				"servers.h: oauth differs from parent",
				"servers.h: url differs from parent",
			},
		},
		{
			name:   "identical url server narrows, headers compared unexpanded",
			parent: "[[servers]]\nname = \"h\"\nurl = \"https://a.example/mcp\"\n[servers.headers]\nAuthorization = \"Bearer $A\"\n",
			child:  "[[servers]]\nname = \"h\"\nurl = \"https://a.example/mcp\"\n[servers.headers]\nAuthorization = \"Bearer $A\"\n",
		},
		{
			name:   "child may add an annotation filter",
			parent: smith,
			child:  smith + "[servers.annotations]\nreadOnlyHint = true\n",
		},
		{
			name:   "child may not drop the parent's annotation filter",
			parent: smith + "[servers.annotations]\nreadOnlyHint = true\n",
			child:  smith,
			want:   []string{"servers.smith: annotations.readOnlyHint = true in parent, not kept by child"},
		},
		{
			name:   "child may not flip the parent's annotation filter",
			parent: smith + "[servers.annotations]\nreadOnlyHint = true\n",
			child:  smith + "[servers.annotations]\nreadOnlyHint = false\n",
			want:   []string{"servers.smith: annotations.readOnlyHint = true in parent, not kept by child"},
		},
		{
			name:   "presentation-only server fields are not rights",
			parent: smith,
			child:  smith + "ephemeral = true\npaginate = true\nprogressive-disclosure = true\ngenerate-resource-tools = true\n",
		},
		{
			name:   "child keeps the parent's moxin disables and may add more",
			parent: `disable-moxins = ["chix", "man.semantic-search"]`,
			child:  `disable-moxins = ["chix", "man.semantic-search", "grit"]`,
		},
		{
			name:   "a whole-moxin disable covers the parent's single-tool disable",
			parent: `disable-moxins = ["man.semantic-search"]`,
			child:  `disable-moxins = ["man"]`,
		},
		{
			name:   "a single-tool disable does not cover the parent's whole-moxin disable",
			parent: `disable-moxins = ["chix"]`,
			child:  `disable-moxins = ["chix.build"]`,
			want:   []string{"disable-moxins: parent disables chix, child does not"},
		},
		{
			name:   "child may not drop a moxin disable",
			parent: `disable-moxins = ["chix", "man.semantic-search"]`,
			child:  "",
			want: []string{
				"disable-moxins: parent disables chix, child does not",
				"disable-moxins: parent disables man.semantic-search, child does not",
			},
		},
		{
			name:   "child may turn the system moxins off",
			parent: "",
			child:  "builtin-native = false",
		},
		{
			name:   "child may not turn the system moxins back on",
			parent: "builtin-native = false",
			child:  "builtin-native = true",
			want:   []string{"builtin-native: parent sets false, child does not"},
		},
		{
			name:   "child leaving builtin-native unset re-enables the system moxins",
			parent: "builtin-native = false",
			child:  "",
			want:   []string{"builtin-native: parent sets false, child does not"},
		},
		{
			name:   "child may keep the system moxins off",
			parent: "builtin-native = false",
			child:  "builtin-native = false",
		},
		{
			name:   "child may omit the parent's credentials",
			parent: "[credentials]\nread = \"vault read {name}\"\n",
			child:  "",
		},
		{
			name:   "child may not introduce credentials commands",
			parent: "",
			child:  "[credentials]\nread = \"vault read {name}\"\n",
			want:   []string{"credentials: not in parent"},
		},
		{
			name:   "child may not change credentials commands",
			parent: "[credentials]\nread = \"vault read {name}\"\n",
			child:  "[credentials]\nread = \"cat /etc/shadow\"\n",
			want:   []string{"credentials: differs from parent"},
		},
		{
			name:   "global presentation flags are not rights",
			parent: "",
			child:  "ephemeral = true\nprogressive-disclosure = true\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := violationLines(Check(mustParse(t, tc.parent), mustParse(t, tc.child)))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("violations:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestCheckOrdersViolationsByKeyThenSubject(t *testing.T) {
	parent := mustParse(t, `
builtin-native = false
disable-moxins = ["zeta", "alpha"]
`)
	child := mustParse(t, `
[[servers]]
name = "b"
command = "b"

[[servers]]
name = "a"
command = "a"
`)
	want := []string{
		"builtin-native: parent sets false, child does not",
		"disable-moxins: parent disables alpha, child does not",
		"disable-moxins: parent disables zeta, child does not",
		"servers.a: not in parent",
		"servers.b: not in parent",
	}
	for i := 0; i < 20; i++ {
		if got := violationLines(Check(parent, child)); !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d:\n got %q\nwant %q", i, got, want)
		}
	}
}
