package moxy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.linenisgreat.com/moxy/pkg/perms"
)

// moxy#264: `restart <moxin>` re-reads the moxin from disk, and what follows
// from its files follows the restart too: the permission tiers the
// batch/async/enforce preflights resolve, and the man:// index of the pages
// it ships. Before, only a full reload refreshed them.
func TestRestartingAMoxinRefreshesItsTiersAndPages(t *testing.T) {
	var dir string
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tool := func(tier string) string {
		return "schema = 3\nresult-type = \"text\"\nperms-request = \"" + tier +
			"\"\ncommand = \"bash\"\nargs = [\"-c\", \"printf ran-flip\"]\n\n[input]\ntype = \"object\"\n"
	}
	inst := openFixture(t, func(o *Options) {
		o.MetaTools = true
		dir = filepath.Join(o.MoxinPath, "flip")
		write("_moxin.toml", "schema = 1\nname = \"flip\"\n")
		write("t.toml", tool("always-allow"))
		write("share/moxy/man/flip.1.txt", "FLIP(1)\n")
	})
	ctx := context.Background()
	enforce := perms.Policy{Mode: perms.Enforce}
	callFlip := func() *Result {
		t.Helper()
		res, err := inst.Call(ctx, Call{Name: "flip.t", Args: json.RawMessage(`{}`), Caller: Caller{Policy: &enforce}})
		if err != nil {
			t.Fatalf("Call flip.t: %v", err)
		}
		return res
	}
	if res := callFlip(); res.IsError || res.Decision != perms.Allow {
		t.Fatalf("flip.t before restart: isError=%v decision=%q", res.IsError, res.Decision)
	}

	write("t.toml", tool("each-use"))
	write("share/moxy/man/flip-more.1.txt", "FLIP-MORE(1)\n")
	res, err := inst.Call(ctx, Call{Name: "restart", Args: json.RawMessage(`{"server":"flip"}`)})
	if err != nil || res.IsError {
		t.Fatalf("restart flip: err=%v result=%+v", err, res)
	}

	if res := callFlip(); !res.IsError || res.Decision != perms.Ask {
		t.Fatalf("flip.t after restart as each-use: isError=%v decision=%q, want refused as ask", res.IsError, res.Decision)
	}
	page, err := inst.ReadResource(ctx, "man://flip-more.1")
	if err != nil {
		t.Fatalf("man://flip-more.1 after restart: %v", err)
	}
	if len(page.Contents) == 0 || !strings.Contains(page.Contents[0].Text, "FLIP-MORE(1)") {
		t.Fatalf("man://flip-more.1 = %+v, want the page added before the restart", page.Contents)
	}
}
