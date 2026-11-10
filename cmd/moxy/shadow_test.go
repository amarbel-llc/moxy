package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// Shadow mode is off unless the operator turns it on: merging it changes
// nothing, and creates no log file.
func TestShadowIsOffByDefault(t *testing.T) {
	home := t.TempDir()
	for _, env := range []map[string]string{{}, {"MOXY_SHADOW": ""}, {"MOXY_SHADOW": "0"}, {"MOXY_SHADOW": "off"}} {
		obs, closeFn := shadowFromEnv(envOf(env), home, "/work", "s1", io.Discard)
		if obs != nil {
			t.Errorf("env %v: observer is on", env)
		}
		closeFn()
	}
	if _, err := os.Stat(filepath.Join(home, ".local")); !os.IsNotExist(err) {
		t.Errorf("off-by-default still touched the log directory: %v", err)
	}
}

func TestShadowOnLogsToAPrivateFileWithoutValues(t *testing.T) {
	home := t.TempDir()
	obs, closeFn := shadowFromEnv(envOf(map[string]string{"MOXY_SHADOW": "1"}), home, "/work", "sess-1", io.Discard)
	if obs == nil {
		t.Fatal("MOXY_SHADOW=1 left shadow mode off")
	}
	const secret = "hunter2-token"
	obs.Observe("man.toc", json.RawMessage(`{"page":"`+secret+`"}`))
	closeFn()

	dir := filepath.Join(home, ".local", "log", "moxy", "shadow")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("shadow dir %s: %v, %v", dir, entries, err)
	}
	path := filepath.Join(dir, entries[0].Name())
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("log mode = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Errorf("log carries an argument value: %s", data)
	}
	if !strings.Contains(string(data), `"tool":"man.toc"`) {
		t.Errorf("log lacks the tool: %s", data)
	}
}

func TestShadowLogOpenFailureLeavesItOff(t *testing.T) {
	home := t.TempDir()
	// A file where the log directory must go makes the open fail.
	if err := os.WriteFile(filepath.Join(home, ".local"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	obs, closeFn := shadowFromEnv(envOf(map[string]string{"MOXY_SHADOW": "1"}), home, "/work", "s1", &stderr)
	defer closeFn()
	if obs != nil {
		t.Error("observer is on although the log could not be opened")
	}
	if !strings.Contains(stderr.String(), "shadow") {
		t.Errorf("no notice on stderr: %q", stderr.String())
	}
}
