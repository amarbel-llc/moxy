package moxy

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/proxy"
	"code.linenisgreat.com/moxy/pkg/moxyfile"
)

// A [[servers]] child whose allow-tools leaves it no tools is not spawned
// (moxy#525, operator ruling q-n4): nothing it could answer is listed or
// callable, so starting it would only cost memory.

// spawnRecorder is a ConnectFunc that records which servers were started.
type spawnRecorder struct {
	mu      sync.Mutex
	started []string
}

type recordedBackend struct{ name string }

func (b recordedBackend) Call(context.Context, string, any) (json.RawMessage, error) {
	return json.RawMessage(`{"tools":[{"name":"one","inputSchema":{"type":"object"}}]}`), nil
}
func (recordedBackend) Notify(string, any) error                 { return nil }
func (recordedBackend) SetOnNotification(func(*jsonrpc.Message)) {}
func (b recordedBackend) Name() string                           { return b.name }
func (recordedBackend) Close() error                             { return nil }

func (r *spawnRecorder) connect(_ context.Context, cfg moxyfile.ServerConfig) (proxy.ServerBackend, *protocol.InitializeResultV1, error) {
	r.mu.Lock()
	r.started = append(r.started, cfg.Name)
	r.mu.Unlock()
	return recordedBackend{name: cfg.Name},
		&protocol.InitializeResultV1{Capabilities: protocol.ServerCapabilitiesV1{Tools: &protocol.ToolsCapability{}}},
		nil
}

func (r *spawnRecorder) wasStarted(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.started {
		if s == name {
			return true
		}
	}
	return false
}

// openWithAllowTools opens an Instance with one child "srv" and the given
// allow-tools (nil = unset), returning what was started and what moxy said.
func openWithAllowTools(t *testing.T, allow []string) (*spawnRecorder, string) {
	t.Helper()
	rec := &spawnRecorder{}
	var stderr bytes.Buffer
	openFixture(t, func(o *Options) {
		o.Connect = rec.connect
		o.Stderr = &stderr
		o.Config = &moxyfile.Config{
			BuiltinNative: boolp(false),
			AllowTools:    allow,
			Servers: []moxyfile.ServerConfig{
				{Name: "srv", Command: moxyfile.MakeCommand("srv-bin")},
			},
		}
	})
	return rec, stderr.String()
}

func TestChildWithEmptyAllowToolsIsNotSpawned(t *testing.T) {
	rec, said := openWithAllowTools(t, []string{})
	if rec.wasStarted("srv") {
		t.Fatal("allow-tools = [] leaves srv no tools; it must not be spawned")
	}
	if !strings.Contains(said, "srv") || !strings.Contains(said, "allow-tools") {
		t.Fatalf("moxy must say why srv was skipped, got stderr:\n%s", said)
	}
}

func TestChildWhoseAllowListMatchesNothingIsNotSpawned(t *testing.T) {
	rec, said := openWithAllowTools(t, []string{"other.tool", "elsewhere"})
	if rec.wasStarted("srv") {
		t.Fatal("an allow list that names nothing of srv must not spawn it")
	}
	if !strings.Contains(said, "allow-tools") {
		t.Fatalf("moxy must say why srv was skipped, got stderr:\n%s", said)
	}
}

func TestChildThatKeepsOneToolIsSpawned(t *testing.T) {
	rec, _ := openWithAllowTools(t, []string{"srv.one"})
	if !rec.wasStarted("srv") {
		t.Fatal("allow-tools = [\"srv.one\"] keeps a tool of srv; it must be spawned")
	}
}

func TestChildAllowedWholeByBareNameIsSpawned(t *testing.T) {
	rec, _ := openWithAllowTools(t, []string{"srv"})
	if !rec.wasStarted("srv") {
		t.Fatal("allow-tools = [\"srv\"] allows all of srv; it must be spawned")
	}
}

func TestChildIsSpawnedWhenAllowToolsIsUnset(t *testing.T) {
	rec, _ := openWithAllowTools(t, nil)
	if !rec.wasStarted("srv") {
		t.Fatal("without allow-tools every child starts, as before")
	}
}
