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
			child:  smith + "nix-devshell = \"github:org/repo\"\n",
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
			name:   "child may keep the parent's annotation filter",
			parent: smith + "[servers.annotations]\nreadOnlyHint = true\nidempotentHint = true\n",
			child:  smith + "[servers.annotations]\nreadOnlyHint = true\n",
		},
		{
			name:   "child may not drop the parent's annotation filter",
			parent: smith + "[servers.annotations]\nreadOnlyHint = true\n",
			child:  smith,
			want:   []string{"servers.smith: annotations: parent filters tools, child does not"},
		},
		{
			name:   "child may not flip the parent's annotation filter",
			parent: smith + "[servers.annotations]\nreadOnlyHint = true\n",
			child:  smith + "[servers.annotations]\nreadOnlyHint = false\n",
			want:   []string{"servers.smith: annotations.readOnlyHint = false in child, not in parent's filter"},
		},
		{
			// A filter passes a tool matching ANY hint, so a hint the parent
			// lacks lists tools the parent hid.
			name:   "child may not add a hint to the parent's annotation filter",
			parent: smith + "[servers.annotations]\nreadOnlyHint = true\n",
			child:  smith + "[servers.annotations]\nreadOnlyHint = true\ndestructiveHint = true\n",
			want:   []string{"servers.smith: annotations.destructiveHint = true in child, not in parent's filter"},
		},
		{
			// An empty filter matches nothing; no filter matches everything.
			name:   "an empty parent filter is not satisfied by an absent child filter",
			parent: smith + "annotations = {}\n",
			child:  smith,
			want:   []string{"servers.smith: annotations: parent filters tools, child does not"},
		},
		{
			name:   "a command path relative to the working directory is refused even when identical",
			parent: "[[servers]]\nname = \"s\"\ncommand = [\"./bin/s\", \"mcp\"]\n",
			child:  "[[servers]]\nname = \"s\"\ncommand = [\"./bin/s\", \"mcp\"]\n",
			want:   []string{"servers.s: command is relative to the working directory"},
		},
		{
			name:   "absolute, home-anchored and PATH-resolved commands are not working-directory relative",
			parent: "[[servers]]\nname = \"a\"\ncommand = \"/bin/a\"\n[[servers]]\nname = \"b\"\ncommand = \"~/bin/b\"\n[[servers]]\nname = \"c\"\ncommand = \"c\"\n[[servers]]\nname = \"d\"\ncommand = \"$D/d\"\n",
			child:  "[[servers]]\nname = \"a\"\ncommand = \"/bin/a\"\n[[servers]]\nname = \"b\"\ncommand = \"~/bin/b\"\n[[servers]]\nname = \"c\"\ncommand = \"c\"\n[[servers]]\nname = \"d\"\ncommand = \"$D/d\"\n",
		},
		{
			name:   "a devshell relative to the working directory is refused even when identical",
			parent: smith + "nix-devshell = \".\"\n",
			child:  smith + "nix-devshell = \".\"\n",
			want:   []string{"servers.smith: nix-devshell is relative to the working directory"},
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
			name:   "child may set an allowlist when the parent has none",
			parent: "",
			child:  `allow-tools = ["grit.status"]`,
		},
		{
			name:   "child allowlist within the parent's narrows",
			parent: `allow-tools = ["grit", "folio.read", "restart"]`,
			child:  `allow-tools = ["grit.status", "folio.read"]`,
		},
		{
			name:   "an empty child allowlist narrows any parent allowlist",
			parent: `allow-tools = ["grit"]`,
			child:  `allow-tools = []`,
		},
		{
			name:   "child may not drop the parent's allowlist",
			parent: `allow-tools = ["grit"]`,
			child:  "",
			want:   []string{"allow-tools: parent sets an allowlist, child does not"},
		},
		{
			name:   "child may not allow what the parent does not",
			parent: `allow-tools = ["grit.status", "folio"]`,
			child:  `allow-tools = ["grit", "folio.read", "restart"]`,
			want: []string{
				"allow-tools: child holds grit at use, parent at none",
				"allow-tools: child holds restart at use, parent at none",
			},
		},
		{
			// The one upward step: a pass holder confers use on its child.
			name:   "child may allow a tool the parent passes",
			parent: "allow-tools = [\"grit.status\"]\npass-tools = [\"smith.issue-create\"]\n",
			child:  `allow-tools = ["smith.issue-create"]`,
		},
		{
			name:   "child may allow one tool of a server the parent passes whole",
			parent: "allow-tools = []\npass-tools = [\"smith\"]\n",
			child:  `allow-tools = ["smith.issue-create", "smith.issue-get"]`,
		},
		{
			name:   "child may not allow a tool the parent holds at neither level",
			parent: "allow-tools = [\"grit.status\"]\npass-tools = [\"smith.issue-create\"]\n",
			child:  `allow-tools = ["smith.issue-close", "grit.commit"]`,
			want: []string{
				"allow-tools: child holds grit.commit at use, parent at none",
				"allow-tools: child holds smith.issue-close at use, parent at none",
			},
		},
		{
			// A bare child entry is the whole server; the parent passes one tool.
			name:   "child may not allow a whole server when the parent passes one of its tools",
			parent: "allow-tools = []\npass-tools = [\"grit.status\"]\n",
			child:  `allow-tools = ["grit"]`,
			want:   []string{"allow-tools: child holds grit at use, parent at none"},
		},
		{
			name:   "child may pass on what the parent passes or allows",
			parent: "allow-tools = [\"grit.status\"]\npass-tools = [\"smith\"]\n",
			child:  "allow-tools = []\npass-tools = [\"smith.issue-create\", \"grit.status\"]\n",
		},
		{
			name:   "child may not pass a tool the parent holds at neither level",
			parent: "allow-tools = [\"grit.status\"]\npass-tools = [\"smith.issue-create\"]\n",
			child:  "allow-tools = []\npass-tools = [\"smith\", \"chix.build\"]\n",
			want: []string{
				"pass-tools: child holds chix.build at pass, parent at none",
				"pass-tools: child holds smith at pass, parent at none",
			},
		},
		{
			name:   "child may drop the parent's pass tools",
			parent: "allow-tools = [\"grit\"]\npass-tools = [\"smith\"]\n",
			child:  `allow-tools = ["grit"]`,
		},
		{
			// A parent with pass-tools always has an allowlist, so the child
			// must have one too, even if it only passes tools on.
			name:   "child may not drop the allowlist of a parent that passes tools",
			parent: "allow-tools = []\npass-tools = [\"smith\"]\n",
			child:  "",
			want:   []string{"allow-tools: parent sets an allowlist, child does not"},
		},
		{
			name:   "child may pass anything when the parent sets no allowlist",
			parent: "",
			child:  "allow-tools = []\npass-tools = [\"smith\"]\n",
		},
		{
			name:   "child moxins a subset of the parent's record narrow",
			parent: "[resolved-moxins]\ngrit = \"/a/grit\"\nfolio = \"/a/folio\"\n",
			child:  "[resolved-moxins]\ngrit = \"/a/grit\"\n",
		},
		{
			name:   "child may not surface a moxin the parent never found",
			parent: "[resolved-moxins]\ngrit = \"/a/grit\"\n",
			child:  "[resolved-moxins]\ngrit = \"/a/grit\"\nsneaky = \"/b/sneaky\"\n",
			want:   []string{"resolved-moxins.sneaky: moxin from /b/sneaky, not in parent"},
		},
		{
			name:   "child may not resolve a moxin name to another directory",
			parent: "[resolved-moxins]\ngrit = \"/a/grit\"\n",
			child:  "[resolved-moxins]\ngrit = \"/b/grit\"\n",
			want:   []string{"resolved-moxins.grit: moxin from /b/grit, parent's is from /a/grit"},
		},
		{
			name:   "an empty parent record admits no child moxin",
			parent: "[resolved-moxins]\n",
			child:  "[resolved-moxins]\ngrit = \"/a/grit\"\n",
			want:   []string{"resolved-moxins.grit: moxin from /a/grit, not in parent"},
		},
		{
			name:   "a moxin the child disables is not held against it",
			parent: "[resolved-moxins]\n",
			child:  "disable-moxins = [\"grit\"]\n[resolved-moxins]\ngrit = \"/a/grit\"\n",
		},
		{
			name:   "disabling one tool of a foreign moxin does not excuse the rest",
			parent: "[resolved-moxins]\n",
			child:  "disable-moxins = [\"grit.commit\"]\n[resolved-moxins]\ngrit = \"/a/grit\"\n",
			want:   []string{"resolved-moxins.grit: moxin from /a/grit, not in parent"},
		},
		{
			name:   "without a parent record moxins are not compared",
			parent: "",
			child:  "[resolved-moxins]\ngrit = \"/b/grit\"\n",
		},
		{
			name:   "without a child record moxins are not compared",
			parent: "[resolved-moxins]\ngrit = \"/a/grit\"\n",
			child:  "",
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

func TestFlakeRefIsWorkingDirRelative(t *testing.T) {
	for ref, want := range map[string]bool{
		".":                 true,
		"./sub":             true,
		"../x":              true,
		"bin/x":             true,
		"a/b/c":             true,
		"path:.":            true,
		"path:./sub":        true,
		"path:sub":          true,
		"git+file:.":        true,
		"file:rel/x":        true,
		"nixpkgs":           false,
		"github:org/repo":   false,
		"git+https://h/r":   false,
		"/abs/flake":        false,
		"~/flake":           false,
		"path:/abs/flake":   false,
		"path:~/flake":      false,
		"git+file:///abs/r": false,
	} {
		if got := flakeRefIsWorkingDirRelative(ref); got != want {
			t.Errorf("flakeRefIsWorkingDirRelative(%q) = %t, want %t", ref, got, want)
		}
	}
}

func TestIdenticalRelativeHelperAndCredentialsCommandsAreRefused(t *testing.T) {
	const web = "[[servers]]\nname = \"h\"\nurl = \"https://a.example/mcp\"\n"
	cases := []struct {
		name, cfg string
		want      []string
	}{
		{
			name: "relative headers-helper",
			cfg:  web + "headers-helper = \"./bin/token --for h\"\n",
			want: []string{"servers.h: headers-helper is relative to the working directory"},
		},
		{
			name: "PATH and absolute headers-helper",
			cfg:  web + "headers-helper = \"token --for ./h\"\n",
		},
		{
			name: "relative credentials commands",
			cfg:  "[credentials]\nread = \"./cred read {name}\"\nwrite = \"/bin/cred write {name}\"\ndelete = \"bin/cred rm {name}\"\n",
			want: []string{
				"credentials: delete is relative to the working directory",
				"credentials: read is relative to the working directory",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := mustParse(t, tc.cfg)
			got := violationLines(Check(cfg, cfg))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
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
