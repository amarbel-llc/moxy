package moxyfile

import (
	"slices"
	"strings"
	"testing"
)

// manifest-command (moxy#525): an opt-in per server. A server that sets it is
// lazy: moxy lists its tools from the command's output and spawns the server
// on the first call. A server that does not set it is exactly as before.

func TestParseManifestCommand(t *testing.T) {
	cfg, err := Parse([]byte(`
[[servers]]
name = "chrest"
command = "chrest mcp"
manifest-command = ["chrest", "manifest"]

[[servers]]
name = "grit"
command = "grit mcp"
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lazy, plain := cfg.Servers[0], cfg.Servers[1]
	if got := lazy.ManifestCommand; !slices.Equal(got, []string{"chrest", "manifest"}) {
		t.Errorf("manifest-command: got %v", got)
	}
	if !lazy.IsLazy() {
		t.Error("a server with manifest-command must be lazy")
	}
	if plain.IsLazy() || len(plain.ManifestCommand) != 0 {
		t.Error("a server without manifest-command must not be lazy")
	}
}

func TestManifestCommandRoundTrip(t *testing.T) {
	doc, err := DecodeConfig([]byte("[[servers]]\nname = \"chrest\"\ncommand = \"chrest mcp\"\nmanifest-command = [\"chrest\", \"manifest\"]\n"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	out, err := doc.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(string(out), "manifest-command") {
		t.Fatalf("manifest-command lost on encode:\n%s", out)
	}
	doc2, err := DecodeConfig(out)
	if err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	if got := doc2.Data().Servers[0].ManifestCommand; !slices.Equal(got, []string{"chrest", "manifest"}) {
		t.Errorf("manifest-command after round-trip: got %v", got)
	}
}

func TestParseRejectsManifestCommandWithoutCommand(t *testing.T) {
	_, err := Parse([]byte("[[servers]]\nname = \"x\"\nmanifest-command = [\"x\", \"manifest\"]\n"))
	if err == nil {
		t.Fatal("expected an error: manifest-command needs a command to start on the first call")
	}
}

func TestParseRejectsManifestCommandOnURLServer(t *testing.T) {
	_, err := Parse([]byte("[[servers]]\nname = \"x\"\nurl = \"https://example.com/mcp\"\nmanifest-command = [\"x\", \"manifest\"]\n"))
	if err == nil {
		t.Fatal("expected an error: manifest-command is for command servers")
	}
}

func TestParseRejectsManifestCommandWithEphemeral(t *testing.T) {
	_, err := Parse([]byte("[[servers]]\nname = \"x\"\ncommand = \"x\"\nephemeral = true\nmanifest-command = [\"x\", \"manifest\"]\n"))
	if err == nil {
		t.Fatal("expected an error: a lazy server stays running after its first call, an ephemeral one does not")
	}
}

func TestParseRejectsManifestCommandWithNixDevshell(t *testing.T) {
	_, err := Parse([]byte("[[servers]]\nname = \"x\"\ncommand = \"x\"\nnix-devshell = \".\"\nmanifest-command = [\"x\", \"manifest\"]\n"))
	if err == nil {
		t.Fatal("expected an error: the manifest command is not run in the devshell")
	}
}
