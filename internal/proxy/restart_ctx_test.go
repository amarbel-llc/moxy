package proxy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/config"
)

// ctxCapturingBootstrapper records the context Reload hands to Bootstrap so a
// test can assert the spawn context is detached from the caller's (request)
// context.
type ctxCapturingBootstrapper struct {
	captured context.Context
	result   *BootstrapResult
}

func (b *ctxCapturingBootstrapper) Bootstrap(ctx context.Context) (*BootstrapResult, error) {
	b.captured = ctx
	return b.result, nil
}

// A persistent child respawned by restartServer is spawned via
// exec.CommandContext(ctx, …); on the HTTP transport ctx is the per-request
// context (r.Context()), which net/http cancels when the response completes.
// If restartServer passes that context straight through, the freshly-spawned
// child is SIGKILL'd moments after restart reports success. restartServer must
// therefore detach the spawn context from request-cancellation. Regression for
// #408.
func TestRestartServerDetachesChildFromRequestCtx(t *testing.T) {
	okRaw, err := json.Marshal(&protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{protocol.TextContentV1("ok")},
	})
	if err != nil {
		t.Fatal(err)
	}

	var spawnCtx context.Context
	p := &Proxy{
		children: []ChildEntry{{
			Client: &fakeBackend{name: "srv", callRaw: okRaw},
			Config: config.ServerConfig{Name: "srv"},
		}},
		configs: map[string]config.ServerConfig{"srv": {Name: "srv"}},
		connectFunc: func(ctx context.Context, _ config.ServerConfig) (ServerBackend, *protocol.InitializeResultV1, error) {
			spawnCtx = ctx
			return &fakeBackend{name: "srv", callRaw: okRaw}, &protocol.InitializeResultV1{}, nil
		},
	}

	reqCtx, cancel := context.WithCancel(context.Background())
	if err := p.restartServer(reqCtx, "srv"); err != nil {
		t.Fatalf("restartServer: %v", err)
	}
	cancel() // simulate the HTTP request completing / r.Context() cancelling

	if spawnCtx == nil {
		t.Fatal("connectFunc was never called")
	}
	select {
	case <-spawnCtx.Done():
		t.Fatal("spawn context cancelled when request context cancelled — respawned child would be SIGKILL'd (#408)")
	default:
	}
}

// The reload-all variant (restart with no server) respawns EVERY persistent
// child via bootstrap under the caller's context. On HTTP that context is
// r.Context(), so completing the reload response would SIGKILL all children at
// once. Reload must detach the bootstrap context from request-cancellation.
// Regression for #408.
func TestReloadDetachesChildrenFromRequestCtx(t *testing.T) {
	bs := &ctxCapturingBootstrapper{result: &BootstrapResult{}}
	p := &Proxy{}
	p.SetBootstrapper(bs)

	reqCtx, cancel := context.WithCancel(context.Background())
	if err := p.Reload(reqCtx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	cancel() // simulate the HTTP request completing / r.Context() cancelling

	if bs.captured == nil {
		t.Fatal("bootstrapper was never called")
	}
	select {
	case <-bs.captured.Done():
		t.Fatal("bootstrap context cancelled when request context cancelled — all children would be SIGKILL'd (#408)")
	default:
	}
}
