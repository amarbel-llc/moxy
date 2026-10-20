package moxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/pkg/moxyfile"
)

// memBlobs is an in-memory BlobStore: enough for Open to run without a
// madder store on disk and for a result's blob reference to be read back.
type memBlobs struct{ blobs map[string]string }

func newMemBlobs() *memBlobs { return &memBlobs{blobs: map[string]string{}} }

func (m *memBlobs) Write(_ context.Context, content io.Reader) (string, error) {
	body, err := io.ReadAll(content)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	digest := "blake2b256-" + hex.EncodeToString(sum[:])
	m.blobs[digest] = string(body)
	return digest, nil
}

func (m *memBlobs) OpenBlob(_ context.Context, digest string) (*os.File, native.BlobWriter, error) {
	payload, ok := m.blobs[digest]
	if !ok {
		return nil, nil, fmt.Errorf("blob %s not found", digest)
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	return pr, &pipeWriter{pw: pw, payload: payload}, nil
}

func (m *memBlobs) CatBytes(_ context.Context, digest string) ([]byte, error) {
	payload, ok := m.blobs[digest]
	if !ok {
		return nil, fmt.Errorf("blob %s not found", digest)
	}
	return []byte(payload), nil
}

type pipeWriter struct {
	pw      *os.File
	payload string
	done    chan error
}

func (w *pipeWriter) Start() error {
	w.done = make(chan error, 1)
	go func() {
		_, err := io.WriteString(w.pw, w.payload)
		w.pw.Close()
		w.done <- err
	}()
	return nil
}

func (w *pipeWriter) Wait() error {
	if w.done == nil {
		return nil
	}
	return <-w.done
}

func (w *pipeWriter) Cleanup() { _ = w.pw.Close() }

// writeFixtureMoxin lays out one moxin named "fix" with three tools: an
// always-allow echo, an each-use tool, and a dynamic tool whose predicate
// always denies — the shapes moxy#485's server_side_perms.bats uses.
func writeFixtureMoxin(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "fix")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("_moxin.toml", "schema = 1\nname = \"fix\"\ndescription = \"pkg/moxy fixture\"\n")
	write("echo.toml", `schema = 3
result-type = "text"
content-type = "text/plain"
perms-request = "always-allow"
description = "prints its argument"
command = "bash"
args = ["-c", "printf '%s' \"$1\"", "_"]

[input]
type = "object"
properties = { text = { type = "string" } }
`)
	write("asky.toml", `schema = 3
result-type = "text"
content-type = "text/plain"
perms-request = "each-use"
description = "each-use tool"
command = "bash"
args = ["-c", "printf 'ran each-use'"]

[input]
type = "object"
`)
	write("dyndeny.toml", `schema = 3
result-type = "text"
content-type = "text/plain"
perms-request = "dynamic"
description = "dynamic tool whose predicate always denies"
command = "bash"
args = ["-c", "printf 'ran dynamic-deny'"]

[input]
type = "object"

[dynamic-perms]
command = "bash"
args = ["-c", "exit 2"]
`)
	return root
}

func boolp(b bool) *bool { return &b }

// openFixture opens an Instance over the fixture moxin alone: an inline
// Config with the bundled moxins off, so neither the test host's moxyfile
// hierarchy nor its system moxins take part.
func openFixture(t *testing.T, mutate func(*Options)) *Instance {
	t.Helper()
	opts := Options{
		Config:    &moxyfile.Config{BuiltinNative: boolp(false)},
		MoxinPath: writeFixtureMoxin(t),
		SessionID: "pkg-moxy-test",
		Blobs:     newMemBlobs(),
	}
	if mutate != nil {
		mutate(&opts)
	}
	inst, err := Open(context.Background(), opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = inst.Close(context.Background()) })
	return inst
}

func TestOpenListsFixtureToolsAndNoMetaToolsByDefault(t *testing.T) {
	inst := openFixture(t, nil)
	tools, err := inst.Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"fix.echo", "fix.asky", "fix.dyndeny"} {
		if !names[want] {
			t.Errorf("tools/list lacks %s; got %v", want, names)
		}
	}
	for _, meta := range []string{"restart", "batch", "async"} {
		if names[meta] {
			t.Errorf("meta tool %s listed without Options.MetaTools", meta)
		}
	}
}

func TestOpenWithMetaToolsListsThem(t *testing.T) {
	inst := openFixture(t, func(o *Options) { o.MetaTools = true })
	tools, err := inst.Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, tool := range tools {
		if tool.Name == "restart" {
			found = true
		}
	}
	if !found {
		t.Fatal("restart meta tool not listed with Options.MetaTools")
	}
}

func TestCallRunsAnAlwaysAllowTool(t *testing.T) {
	inst := openFixture(t, nil)
	res, err := inst.Call(context.Background(), Call{
		Name: "fix.echo",
		Args: json.RawMessage(`{"text":"hello from pkg/moxy"}`),
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if res.IsError {
		t.Fatalf("Call returned isError: %+v", res.Content)
	}
	var text string
	for _, b := range res.Content {
		text += b.Text
	}
	if !strings.Contains(text, "hello from pkg/moxy") {
		t.Fatalf("content = %q, want the echoed text", text)
	}
}

func TestServersReportsTheFixtureRunning(t *testing.T) {
	inst := openFixture(t, nil)
	var seen bool
	for _, s := range inst.Servers(context.Background()) {
		if s.Name == "fix" && s.Status == "running" && s.Tools == 3 {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("Servers = %+v, want fix running with 3 tools", inst.Servers(context.Background()))
	}
}

func TestCloseMakesEveryMethodReturnErrClosed(t *testing.T) {
	inst := openFixture(t, nil)
	if err := inst.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := inst.Tools(context.Background()); err != ErrClosed {
		t.Errorf("Tools after Close = %v, want ErrClosed", err)
	}
	if _, err := inst.Call(context.Background(), Call{Name: "fix.echo"}); err != ErrClosed {
		t.Errorf("Call after Close = %v, want ErrClosed", err)
	}
	if err := inst.Close(context.Background()); err != ErrClosed {
		t.Errorf("second Close = %v, want ErrClosed", err)
	}
}

func TestOpenRefusesAMoxyfileThatWidensTheParent(t *testing.T) {
	parent := &moxyfile.Config{
		BuiltinNative:  boolp(false),
		AllowTools:     []string{"fix.echo"},
		ResolvedMoxins: map[string]string{},
	}
	_, err := Open(context.Background(), Options{
		Config:    &moxyfile.Config{BuiltinNative: boolp(false), AllowTools: []string{"fix"}},
		MoxinPath: writeFixtureMoxin(t),
		Blobs:     newMemBlobs(),
		Parent:    parent,
	})
	var widening *WideningError
	if !errors.As(err, &widening) {
		t.Fatalf("Open = %v, want *WideningError", err)
	}
	if len(widening.Violations) == 0 {
		t.Fatal("WideningError carries no violations")
	}
}

func TestBlobRefsInFindsEveryReferenceOnce(t *testing.T) {
	refs := blobRefsIn([]protocol.ContentBlockV1{
		{Type: "text", Text: "see madder://blobs/blake2b256-abc and madder://blobs/blake2b256-abc again"},
		{Type: "resource_link", URI: "madder://blobs/blake2b256-def"},
	})
	if len(refs) != 2 {
		t.Fatalf("refs = %+v, want 2 distinct", refs)
	}
	if refs[0].Digest != "blake2b256-abc" || refs[1].Digest != "blake2b256-def" {
		t.Fatalf("refs = %+v", refs)
	}
}
