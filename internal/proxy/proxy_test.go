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

func TestPavedPathToolAllowedNoPaths(t *testing.T) {
	p := &Proxy{}
	if !p.pavedPathToolAllowed("folio.read") {
		t.Error("expected all tools allowed when no paved paths configured")
	}
}

func TestPavedPathToolAllowedNoStateSelected(t *testing.T) {
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{{
			Name: "onboarding",
			Stages: []config.PavedPathStage{
				{Label: "orient", Tools: []string{"folio.read"}},
			},
		}},
	}
	// paved-paths meta tool always allowed
	if !p.pavedPathToolAllowed(config.PavedPathsToolName) {
		t.Error("paved-paths tool should always be allowed")
	}
	// other tools blocked before path selection
	if p.pavedPathToolAllowed("folio.read") {
		t.Error("folio.read should be blocked before path selection")
	}
}

func TestPavedPathToolAllowedStage(t *testing.T) {
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{{
			Name: "onboarding",
			Stages: []config.PavedPathStage{
				{Label: "orient", Tools: []string{"folio.read"}},
				{Label: "edit", Tools: []string{"folio.write"}},
			},
		}},
		pavedPathState: &pavedPathState{
			SelectedPath: "onboarding",
			CurrentStage: 0,
			CalledTools:  make(map[string]bool),
		},
	}
	if !p.pavedPathToolAllowed("folio.read") {
		t.Error("folio.read should be allowed in stage 0")
	}
	if p.pavedPathToolAllowed("folio.write") {
		t.Error("folio.write should be blocked in stage 0")
	}
}

func TestPavedPathToolAllowedComplete(t *testing.T) {
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{{
			Name: "onboarding",
			Stages: []config.PavedPathStage{
				{Label: "orient", Tools: []string{"folio.read"}},
			},
		}},
		pavedPathState: &pavedPathState{
			SelectedPath: "onboarding",
			Complete:     true,
		},
	}
	if !p.pavedPathToolAllowed("any.tool") {
		t.Error("all tools should be allowed when path is complete")
	}
}
