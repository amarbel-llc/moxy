package proxy

import (
	"testing"

	"github.com/amarbel-llc/moxy/internal/config"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
)

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

func TestPavedPathStateInitial(t *testing.T) {
	p := &Proxy{}
	if p.pavedPathState != nil {
		t.Error("expected nil paved path state initially")
	}
}

func TestNoPavedPathsNotActive(t *testing.T) {
	p := &Proxy{}
	if p.pavedPathsActive() {
		t.Error("expected pavedPathsActive false when no paths configured")
	}
}

func TestPavedPathsActive(t *testing.T) {
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{
			{Name: "onboarding"},
		},
	}
	if !p.pavedPathsActive() {
		t.Error("expected pavedPathsActive true when paths configured")
	}
}
