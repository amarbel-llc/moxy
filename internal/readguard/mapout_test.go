package readguard

import (
	"strings"
	"testing"
)

func TestMapOutput(t *testing.T) {
	cases := []struct {
		name, orig, repl, in, want string
	}{
		{"fd anywhere", "a/b.txt", "/dev/fd/5", "/dev/fd/5: ASCII text\nx /dev/fd/5 y\n", "a/b.txt: ASCII text\nx a/b.txt y\n"},
		{"dir root line", "sub", "./", ".\n├── a\n", "sub\n├── a\n"},
		{"ls header", "sub", "./", ".:\na\n\n./d:\nb\n", "sub:\na\n\nsub/d:\nb\n"},
		{"file(1)", "sub", "./", ".: directory\n", "sub: directory\n"},
		{"trailing slash operand", "sub/", "./", "./a\n", "sub/a\n"},
		{"du", "sub", "./", "4\t./a\n8\t.\n", "4\tsub/a\n8\tsub\n"},
		{"tree json", "sub", "./", `[{"type":"directory","name":".","contents":[]}]` + "\n", `[{"type":"directory","name":"sub","contents":[]}]` + "\n"},
		{"dot operand is a no-op", ".", "./", "./a\n.\n", "./a\n.\n"},
		{"root", "/", "./", "./etc\n", "/etc\n"},
		{"untouched", "sub", "./", "plain ./text\n.hidden\n", "plain ./text\n.hidden\n"},
		{"no trailing newline", "sub", "./", ".", "sub"},
	}
	for _, c := range cases {
		var out strings.Builder
		if err := MapOutput(&out, strings.NewReader(c.in), c.orig, c.repl); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if out.String() != c.want {
			t.Errorf("%s: got %q, want %q", c.name, out.String(), c.want)
		}
	}
}
