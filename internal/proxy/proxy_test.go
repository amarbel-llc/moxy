package proxy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"code.linenisgreat.com/moxy/internal/config"
	"code.linenisgreat.com/moxy/internal/native"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
)

type fakeMoxinReloader struct {
	cfg *native.NativeConfig
	err error
}

func (f *fakeMoxinReloader) ReloadMoxin(name string) (*native.NativeConfig, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.cfg, nil
}

func TestRestartMoxinSwapsChildAndNotifies(t *testing.T) {
	original := native.NewServer(&native.NativeConfig{
		Name:        "fixture",
		Description: "fixture moxin (original)",
		Tools:       []native.ToolSpec{{Name: "v1", Description: "first version"}},
	})

	var notified int
	p := &Proxy{
		children: []ChildEntry{{Client: original, Config: config.ServerConfig{Name: "fixture"}}},
		notifier: func(msg *jsonrpc.Message) error {
			notified++
			return nil
		},
	}
	p.SetSessionID("test-session")
	p.SetMoxinReloader(&fakeMoxinReloader{
		cfg: &native.NativeConfig{
			Name:        "fixture",
			Description: "fixture moxin (reloaded)",
			Tools: []native.ToolSpec{
				{Name: "v1", Description: "first version"},
				{Name: "v2", Description: "added by reload"},
			},
		},
	})

	if err := p.restartServer(context.Background(), "fixture"); err != nil {
		t.Fatalf("restartServer: %v", err)
	}

	if notified != 1 {
		t.Errorf("expected 1 tools/listChanged notification, got %d", notified)
	}

	if len(p.children) != 1 {
		t.Fatalf("expected 1 child after restart, got %d", len(p.children))
	}
	swapped, ok := p.children[0].Client.(*native.Server)
	if !ok {
		t.Fatalf("expected swapped child to be *native.Server, got %T", p.children[0].Client)
	}
	if swapped == original {
		t.Error("expected restartServer to install a fresh *native.Server, not reuse the original")
	}
	if swapped.Name() != "fixture" {
		t.Errorf("expected swapped server name fixture, got %q", swapped.Name())
	}
	if got := p.children[0].Instructions; got != "fixture moxin (reloaded)" {
		t.Errorf("expected instructions to come from reloaded config, got %q", got)
	}
}

func TestRestartMoxinReloaderError(t *testing.T) {
	original := native.NewServer(&native.NativeConfig{Name: "fixture"})

	p := &Proxy{
		children: []ChildEntry{{Client: original, Config: config.ServerConfig{Name: "fixture"}}},
	}
	p.SetMoxinReloader(&fakeMoxinReloader{err: errors.New("disk on fire")})

	err := p.restartServer(context.Background(), "fixture")
	if err == nil {
		t.Fatal("expected error from restartServer when reloader fails")
	}
	if !strings.Contains(err.Error(), "disk on fire") {
		t.Errorf("expected error to wrap reloader error, got %v", err)
	}
}

func TestRestartMoxinNoReloaderConfigured(t *testing.T) {
	original := native.NewServer(&native.NativeConfig{Name: "fixture"})

	p := &Proxy{
		children: []ChildEntry{{Client: original, Config: config.ServerConfig{Name: "fixture"}}},
	}

	err := p.restartServer(context.Background(), "fixture")
	if err == nil {
		t.Fatal("expected error when no MoxinReloader is configured")
	}
	if !strings.Contains(err.Error(), "moxin reloader") {
		t.Errorf("expected error to mention moxin reloader, got %v", err)
	}
}

type fakeBootstrapper struct {
	result *BootstrapResult
	err    error
	calls  int
}

func (f *fakeBootstrapper) Bootstrap(ctx context.Context) (*BootstrapResult, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestReloadSwapsAllChildrenAndNotifies(t *testing.T) {
	originalMoxin := native.NewServer(&native.NativeConfig{
		Name:  "alpha",
		Tools: []native.ToolSpec{{Name: "v1"}},
	})

	var notified int
	p := &Proxy{
		children: []ChildEntry{
			{Client: originalMoxin, Config: config.ServerConfig{Name: "alpha"}},
		},
		ephemeral: make(map[string]*EphemeralMeta),
		notifier: func(msg *jsonrpc.Message) error {
			notified++
			return nil
		},
	}

	freshMoxin := native.NewServer(&native.NativeConfig{
		Name:  "alpha",
		Tools: []native.ToolSpec{{Name: "v1"}, {Name: "v2"}},
	})
	beta := native.NewServer(&native.NativeConfig{
		Name:  "beta",
		Tools: []native.ToolSpec{{Name: "v1"}},
	})
	p.SetBootstrapper(&fakeBootstrapper{
		result: &BootstrapResult{
			Children: []ChildEntry{
				{Client: freshMoxin, Config: config.ServerConfig{Name: "alpha"}},
				{Client: beta, Config: config.ServerConfig{Name: "beta"}},
			},
		},
	})

	if err := p.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if notified != 1 {
		t.Errorf("expected 1 tools/listChanged notification, got %d", notified)
	}
	if len(p.children) != 2 {
		t.Fatalf("expected 2 children after reload, got %d", len(p.children))
	}
	if p.children[0].Client != freshMoxin {
		t.Error("expected first child to be the fresh alpha")
	}
	if p.children[1].Client != beta {
		t.Error("expected second child to be the new beta")
	}
}

func TestReloadFailureLeavesStateIntact(t *testing.T) {
	originalMoxin := native.NewServer(&native.NativeConfig{Name: "alpha"})
	p := &Proxy{
		children: []ChildEntry{
			{Client: originalMoxin, Config: config.ServerConfig{Name: "alpha"}},
		},
	}
	p.SetBootstrapper(&fakeBootstrapper{err: errors.New("disk on fire")})

	if err := p.Reload(context.Background()); err == nil {
		t.Fatal("expected error from Reload when bootstrapper fails")
	}
	if len(p.children) != 1 || p.children[0].Client != originalMoxin {
		t.Error("expected children unchanged on bootstrapper failure")
	}
}

func TestReloadNoBootstrapperConfigured(t *testing.T) {
	p := &Proxy{}
	err := p.Reload(context.Background())
	if err == nil {
		t.Fatal("expected error when no Bootstrapper is configured")
	}
	if !strings.Contains(err.Error(), "bootstrapper") {
		t.Errorf("expected error to mention bootstrapper, got %v", err)
	}
}

func TestHandleRestartEmptyServerCallsReload(t *testing.T) {
	originalMoxin := native.NewServer(&native.NativeConfig{Name: "alpha"})
	freshMoxin := native.NewServer(&native.NativeConfig{Name: "alpha"})

	var notified int
	p := &Proxy{
		children: []ChildEntry{
			{Client: originalMoxin, Config: config.ServerConfig{Name: "alpha"}},
		},
		ephemeral: make(map[string]*EphemeralMeta),
		notifier: func(msg *jsonrpc.Message) error {
			notified++
			return nil
		},
	}
	bs := &fakeBootstrapper{
		result: &BootstrapResult{
			Children: []ChildEntry{{Client: freshMoxin, Config: config.ServerConfig{Name: "alpha"}}},
		},
	}
	p.SetBootstrapper(bs)

	for _, args := range []string{``, `{}`, `{"server":""}`} {
		bs.calls = 0
		notified = 0

		result, err := p.HandleRestart(context.Background(), []byte(args))
		if err != nil {
			t.Fatalf("HandleRestart(%q): %v", args, err)
		}
		if result.IsError {
			t.Errorf("HandleRestart(%q) returned error result: %+v", args, result.Content)
		}
		if bs.calls != 1 {
			t.Errorf("HandleRestart(%q): expected bootstrapper called once, got %d", args, bs.calls)
		}
		if notified != 1 {
			t.Errorf("HandleRestart(%q): expected 1 notification, got %d", args, notified)
		}
		if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "Reloaded") {
			t.Errorf("HandleRestart(%q): expected Reloaded message, got %+v", args, result.Content)
		}
	}
}

func TestNotifyToolsChangedCallsNotifier(t *testing.T) {
	var called int
	p := &Proxy{
		notifier: func(msg *jsonrpc.Message) error {
			called++
			return nil
		},
	}
	p.notifyToolsChanged()
	if called != 1 {
		t.Errorf("expected notifier called 1 time, got %d", called)
	}
}

func TestNotifyToolsChangedNoNotifier(t *testing.T) {
	p := &Proxy{}
	// Must not panic when notifier is nil
	p.notifyToolsChanged()
}
