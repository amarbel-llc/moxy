package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/pkg/moxyfile"
)

// A lazy server (manifest-command, moxy#525) is not spawned at startup: its
// tools come from the manifest command, and the first call spawns it and keeps
// it running. A server without a manifest-command behaves exactly as before.

const manifestToolsJSON = `{"tools":[{"name":"hello","description":"says hello","inputSchema":{"type":"object"}}]}`

// TestLazyManifestHelper is not a test: the lazy tests run the test binary as
// their manifest-command, selected by MOXY_TEST_LAZY_MANIFEST. Without it, it
// does nothing.
func TestLazyManifestHelper(t *testing.T) {
	switch os.Getenv("MOXY_TEST_LAZY_MANIFEST") {
	case "ok":
		fmt.Print(manifestToolsJSON)
		os.Exit(0)
	case "garbage":
		fmt.Print("not json at all")
		os.Exit(0)
	case "fail":
		fmt.Fprint(os.Stderr, "manifest exploded")
		os.Exit(3)
	}
}

func lazyConfig(t *testing.T, mode string) moxyfile.ServerConfig {
	t.Helper()
	t.Setenv("MOXY_TEST_LAZY_MANIFEST", mode)
	return moxyfile.ServerConfig{
		Name:            "lazy",
		Command:         moxyfile.MakeCommand("lazy-server"),
		ManifestCommand: []string{os.Args[0], "-test.run=^TestLazyManifestHelper$"},
	}
}

// lazyTestBackend counts what happens to a spawned server.
type lazyTestBackend struct {
	mu     sync.Mutex
	raw    json.RawMessage
	calls  []string
	closed bool
}

func (b *lazyTestBackend) Call(_ context.Context, method string, _ any) (json.RawMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, method)
	return b.raw, nil
}
func (b *lazyTestBackend) Notify(string, any) error                 { return nil }
func (b *lazyTestBackend) SetOnNotification(func(*jsonrpc.Message)) {}
func (b *lazyTestBackend) Name() string                             { return "lazy" }
func (b *lazyTestBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

func (b *lazyTestBackend) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func (b *lazyTestBackend) toolCalls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, m := range b.calls {
		if m == protocol.MethodToolsCall {
			n++
		}
	}
	return n
}

// spawnCounter is a ConnectFunc that records every spawn.
type spawnCounter struct {
	mu       sync.Mutex
	backends []*lazyTestBackend
	raw      json.RawMessage
	caps     protocol.ServerCapabilitiesV1
}

func (s *spawnCounter) connect(context.Context, moxyfile.ServerConfig) (ServerBackend, *protocol.InitializeResultV1, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := &lazyTestBackend{raw: s.raw}
	s.backends = append(s.backends, b)
	return b, &protocol.InitializeResultV1{Capabilities: s.caps}, nil
}

func (s *spawnCounter) spawns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.backends)
}

func okCallResult(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(&protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{protocol.TextContentV1("ok")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func toolNames(t *testing.T, p *Proxy) []string {
	t.Helper()
	res, err := p.ListToolsV1(context.Background(), "")
	if err != nil {
		t.Fatalf("ListToolsV1: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestLazyServerIsNotSpawnedAtBoot(t *testing.T) {
	sc := &spawnCounter{}
	p := New(nil, nil, []moxyfile.ServerConfig{lazyConfig(t, "ok")}, nil, nil, sc.connect)
	p.ProbeEphemeral(context.Background())

	if n := sc.spawns(); n != 0 {
		t.Fatalf("a lazy server must not be spawned at boot, got %d spawn(s)", n)
	}
	if names := toolNames(t, p); !slices.Contains(names, "lazy.hello") {
		t.Fatalf("tools/list must answer from the manifest: got %v", names)
	}
}

func TestLazyServerFirstCallSpawnsOnceAndKeepsRunning(t *testing.T) {
	sc := &spawnCounter{raw: okCallResult(t)}
	p := New(nil, nil, []moxyfile.ServerConfig{lazyConfig(t, "ok")}, nil, nil, sc.connect)
	p.ProbeEphemeral(context.Background())

	for i := 0; i < 2; i++ {
		res, err := p.CallToolV1(context.Background(), "lazy.hello", nil)
		if err != nil || res.IsError {
			t.Fatalf("call %d: res=%+v err=%v", i, res, err)
		}
	}
	if n := sc.spawns(); n != 1 {
		t.Fatalf("the first call spawns the server once and the second reuses it: got %d spawn(s)", n)
	}
	b := sc.backends[0]
	if b.isClosed() {
		t.Fatal("a lazy server stays running after its first call")
	}
	if n := b.toolCalls(); n != 2 {
		t.Fatalf("both calls must reach the one running server: got %d", n)
	}

	p.CloseChildren()
	if !b.isClosed() {
		t.Fatal("shutdown must stop a running lazy server")
	}
}

func TestLazyServerRestartStopsItAndRereadsManifest(t *testing.T) {
	sc := &spawnCounter{raw: okCallResult(t)}
	p := New(nil, nil, []moxyfile.ServerConfig{lazyConfig(t, "ok")}, nil, nil, sc.connect)
	p.ProbeEphemeral(context.Background())
	if _, err := p.CallToolV1(context.Background(), "lazy.hello", nil); err != nil {
		t.Fatal(err)
	}

	if err := p.RestartServer(context.Background(), "lazy"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if !sc.backends[0].isClosed() {
		t.Fatal("restart must stop the running lazy server")
	}
	if _, err := p.CallToolV1(context.Background(), "lazy.hello", nil); err != nil {
		t.Fatal(err)
	}
	if n := sc.spawns(); n != 2 {
		t.Fatalf("the call after a restart starts the server again: got %d spawn(s)", n)
	}
}

func TestLazyServerWithFailingManifestIsALoadError(t *testing.T) {
	for _, mode := range []string{"fail", "garbage"} {
		t.Run(mode, func(t *testing.T) {
			sc := &spawnCounter{}
			p := New(nil, nil, []moxyfile.ServerConfig{lazyConfig(t, mode)}, nil, nil, sc.connect)
			p.ProbeEphemeral(context.Background())

			if n := sc.spawns(); n != 0 {
				t.Fatalf("a failing manifest must not fall back to spawning: got %d spawn(s)", n)
			}
			var failed []string
			for _, f := range p.failed {
				failed = append(failed, f.Name)
			}
			if !slices.Contains(failed, "lazy") {
				t.Fatalf("the server must be reported as failed, got %v", failed)
			}
		})
	}
}

func TestServerWithoutManifestCommandBehavesAsBefore(t *testing.T) {
	sc := &spawnCounter{
		raw:  json.RawMessage(manifestToolsJSON),
		caps: protocol.ServerCapabilitiesV1{Tools: &protocol.ToolsCapability{}},
	}
	yes := true
	cfg := moxyfile.ServerConfig{Name: "eph", Command: moxyfile.MakeCommand("eph-server"), Ephemeral: &yes}
	p := New(nil, nil, []moxyfile.ServerConfig{cfg}, nil, nil, sc.connect)
	p.ProbeEphemeral(context.Background())

	if n := sc.spawns(); n != 1 {
		t.Fatalf("an ephemeral server is still probed by spawning it at boot: got %d spawn(s)", n)
	}
	if !sc.backends[0].isClosed() {
		t.Fatal("the probe's server is shut down again, as before")
	}
}
