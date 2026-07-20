package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/config"
)

// probeBackend is a non-native ServerBackend whose Call fails, simulating a
// child that completed `initialize` but whose stdio pipe was already closed by
// the time moxy enumerates its tools ("write |1: file already closed").
type probeBackend struct {
	name    string
	callErr error
	closed  bool
}

func (b *probeBackend) Call(context.Context, string, any) (json.RawMessage, error) {
	return nil, b.callErr
}
func (b *probeBackend) Notify(string, any) error                 { return nil }
func (b *probeBackend) SetOnNotification(func(*jsonrpc.Message)) {}
func (b *probeBackend) Name() string                             { return b.name }
func (b *probeBackend) Close() error                             { b.closed = true; return nil }

// Regression for #405 (failure mode 2): restartServer reported
// "restarted successfully" as soon as `initialize` returned, without confirming
// the child survived the subsequent `tools/list`. A child that init'd but then
// dropped its stdio pipe would be swapped in and reported healthy, and the
// closed-pipe error only surfaced later on the first real tools/list. restart
// must instead probe tools/list before declaring success and surface that
// error. On probe failure the old (healthy) child must stay in place.
func TestRestartServerProbesToolsListBeforeSuccess(t *testing.T) {
	oldClient := &fakeBackend{name: "srv"}
	newClient := &probeBackend{
		name:    "srv",
		callErr: errors.New("write |1: file already closed"),
	}

	p := &Proxy{
		children: []ChildEntry{{
			Client:       oldClient,
			Config:       config.ServerConfig{Name: "srv"},
			Capabilities: protocol.ServerCapabilitiesV1{Tools: &protocol.ToolsCapability{}},
		}},
		configs: map[string]config.ServerConfig{"srv": {Name: "srv"}},
		connectFunc: func(context.Context, config.ServerConfig) (ServerBackend, *protocol.InitializeResultV1, error) {
			return newClient, &protocol.InitializeResultV1{
				Capabilities: protocol.ServerCapabilitiesV1{Tools: &protocol.ToolsCapability{}},
			}, nil
		},
	}

	err := p.restartServer(context.Background(), "srv")
	if err == nil {
		t.Fatal("restartServer returned nil, want closed-pipe error from tools/list probe")
	}
	if !strings.Contains(err.Error(), "file already closed") {
		t.Fatalf("restartServer error = %q, want it to surface the closed-pipe error", err)
	}

	// The healthy old child must still be serving — a failed probe must not
	// tear it down, mirroring the connectFunc-failure branch.
	p.mu.RLock()
	child, ok := findChildIn(p.children, "srv")
	p.mu.RUnlock()
	if !ok {
		t.Fatal("srv missing from children after failed restart; old child was torn down")
	}
	if child.Client != oldClient {
		t.Fatal("old child was replaced by the dead new child after a failed probe")
	}
	if !newClient.closed {
		t.Error("dead new child was not closed after a failed probe (leak)")
	}
}
