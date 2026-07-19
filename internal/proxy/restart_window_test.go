package proxy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/config"
)

// fakeBackend is a minimal NON-native ServerBackend, so a restart routes
// through restartServer (the subprocess path) rather than restartMoxin. Its
// Call returns a fixed raw result.
type fakeBackend struct {
	name    string
	callRaw json.RawMessage
}

func (f *fakeBackend) Call(context.Context, string, any) (json.RawMessage, error) {
	return f.callRaw, nil
}
func (f *fakeBackend) Notify(string, any) error                 { return nil }
func (f *fakeBackend) SetOnNotification(func(*jsonrpc.Message)) {}
func (f *fakeBackend) Name() string                             { return f.name }
func (f *fakeBackend) Close() error                             { return nil }

// Regression for #351: a tool call dispatched concurrently with a server's
// own restart must keep routing to that server. restartServer used to remove
// the old child BEFORE the slow respawn (connectFunc), leaving a window where
// the server was absent from p.children entirely; a pipelined call routed into
// that window got "unknown server". Under CPU contention the respawn outran
// the bats harness's fixed `sleep 1`, so `restart_then_tool_call_works` flaked.
// This drives the window deterministically: the respawn blocks while we fire
// the concurrent call.
func TestRestartServerKeepsServerRoutableDuringRespawn(t *testing.T) {
	okRaw, err := json.Marshal(&protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{protocol.TextContentV1("ok")},
	})
	if err != nil {
		t.Fatal(err)
	}

	respawnStarted := make(chan struct{})
	respawnRelease := make(chan struct{})
	p := &Proxy{
		children: []ChildEntry{{
			Client: &fakeBackend{name: "srv", callRaw: okRaw},
			Config: config.ServerConfig{Name: "srv"},
		}},
		configs: map[string]config.ServerConfig{"srv": {Name: "srv"}},
		connectFunc: func(context.Context, config.ServerConfig) (ServerBackend, *protocol.InitializeResultV1, error) {
			close(respawnStarted)
			<-respawnRelease
			return &fakeBackend{name: "srv", callRaw: okRaw}, &protocol.InitializeResultV1{}, nil
		},
	}

	done := make(chan error, 1)
	go func() { done <- p.restartServer(context.Background(), "srv") }()

	<-respawnStarted // restart is mid-respawn (old child removed under the buggy code)

	// Fire the concurrent tool call while the respawn is in flight, capture
	// the result, THEN release the respawn and wait — so no goroutine leaks
	// even on a failing assertion.
	res, callErr := p.CallToolV1(context.Background(), "srv.exec", nil)
	close(respawnRelease)
	if err := <-done; err != nil {
		t.Fatalf("restartServer: %v", err)
	}

	if callErr != nil {
		t.Fatalf("CallToolV1: %v", callErr)
	}
	if res.IsError && strings.Contains(res.Content[0].Text, "unknown server") {
		t.Fatalf("tool call hit the restart absence window: %q", res.Content[0].Text)
	}
}
