package moxy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moxy#439: a moxy whose working directory is deleted under it (a subagent
// removing its scratch dir) can still be restarted from inside, and after
// the restart its tools run in a directory that exists.
func TestRestartRecoversFromADeletedWorkingDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "moxyfile"), []byte("builtin-native = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(home, "worktree", ".tmp", "scratch")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)

	inst, err := Open(context.Background(), Options{
		MoxinPath: writeFixtureMoxin(t),
		SessionID: "pkg-moxy-test",
		Blobs:     newMemBlobs(),
		MetaTools: true,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = inst.Close(context.Background()) })

	if err := os.RemoveAll(work); err != nil {
		t.Fatal(err)
	}

	text := func(res *Result) string {
		var s string
		for _, b := range res.Content {
			s += b.Text
		}
		return s
	}
	res, err := inst.Call(context.Background(), Call{Name: "restart", Args: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if res.IsError {
		t.Fatalf("restart with a deleted cwd failed: %q", text(res))
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("after restart the working directory is still gone: %v", err)
	}
	if want := filepath.Join(home, "worktree", ".tmp"); cwd != want {
		t.Errorf("after restart cwd = %q, want the nearest existing ancestor %q", cwd, want)
	}

	res, err = inst.Call(context.Background(), Call{Name: "fix.echo", Args: json.RawMessage(`{"text":"still works"}`)})
	if err != nil || res.IsError || !strings.Contains(text(res), "still works") {
		t.Fatalf("fix.echo after restart: err=%v result=%+v", err, res)
	}
}
