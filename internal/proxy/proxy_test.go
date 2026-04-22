package proxy

import (
	"strings"
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

func TestHandlePavedPathsNoPathsConfigured(t *testing.T) {
	p := &Proxy{}
	result := p.HandlePavedPaths(nil)
	if !strings.Contains(result, "no paved paths") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandlePavedPathsListPaths(t *testing.T) {
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{{
			Name:        "onboarding",
			Description: "Learn the repo",
			Stages: []config.PavedPathStage{
				{Label: "orient", Tools: []string{"folio.read"}},
			},
		}},
	}
	result := p.HandlePavedPaths(nil)
	if !strings.Contains(result, "onboarding") {
		t.Errorf("expected path name in result: %s", result)
	}
	if !strings.Contains(result, "Learn the repo") {
		t.Errorf("expected description in result: %s", result)
	}
}

func TestHandlePavedPathsSelectPath(t *testing.T) {
	notified := false
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{{
			Name:        "onboarding",
			Description: "Learn the repo",
			Stages: []config.PavedPathStage{
				{Label: "orient", Tools: []string{"folio.read", "folio.glob"}},
			},
		}},
		notifier: func(msg *jsonrpc.Message) error {
			notified = true
			return nil
		},
	}
	result := p.HandlePavedPaths(map[string]any{"select": "onboarding"})
	if p.pavedPathState == nil {
		t.Fatal("expected pavedPathState to be set")
	}
	if p.pavedPathState.SelectedPath != "onboarding" {
		t.Errorf("expected SelectedPath onboarding, got %s", p.pavedPathState.SelectedPath)
	}
	if !notified {
		t.Error("expected tools/listChanged notification")
	}
	if !strings.Contains(result, "orient") {
		t.Errorf("expected stage label in result: %s", result)
	}
}

func TestHandlePavedPathsSelectUnknownPath(t *testing.T) {
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{{
			Name: "onboarding",
			Stages: []config.PavedPathStage{
				{Label: "orient", Tools: []string{"folio.read"}},
			},
		}},
	}
	result := p.HandlePavedPaths(map[string]any{"select": "nonexistent"})
	if !strings.Contains(result, "unknown") {
		t.Errorf("expected 'unknown' in result: %s", result)
	}
	if p.pavedPathState != nil {
		t.Error("expected pavedPathState to remain nil")
	}
}

func TestMaybeAdvanceStageNoState(t *testing.T) {
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{{
			Name: "onboarding",
			Stages: []config.PavedPathStage{
				{Label: "orient", Tools: []string{"folio.read"}},
			},
		}},
	}
	// must not panic when state is nil
	p.maybeAdvanceStage("folio.read")
}

func TestMaybeAdvanceStageRecordsTool(t *testing.T) {
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{{
			Name: "onboarding",
			Stages: []config.PavedPathStage{
				{Label: "orient", Tools: []string{"folio.read", "folio.glob"}},
				{Label: "edit", Tools: []string{"folio.write"}},
			},
		}},
		pavedPathState: &pavedPathState{
			SelectedPath: "onboarding",
			CurrentStage: 0,
			CalledTools:  make(map[string]bool),
		},
	}
	p.maybeAdvanceStage("folio.read")
	if !p.pavedPathState.CalledTools["folio.read"] {
		t.Error("expected folio.read recorded in CalledTools")
	}
	// first call in stage advances to next stage
	if p.pavedPathState.CurrentStage != 1 {
		t.Errorf("expected CurrentStage 1, got %d", p.pavedPathState.CurrentStage)
	}
}

func TestMaybeAdvanceStageCompletes(t *testing.T) {
	notified := 0
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{{
			Name: "onboarding",
			Stages: []config.PavedPathStage{
				{Label: "orient", Tools: []string{"folio.read"}},
			},
		}},
		pavedPathState: &pavedPathState{
			SelectedPath: "onboarding",
			CurrentStage: 0,
			CalledTools:  make(map[string]bool),
		},
		notifier: func(msg *jsonrpc.Message) error {
			notified++
			return nil
		},
	}
	p.maybeAdvanceStage("folio.read")
	if !p.pavedPathState.Complete {
		t.Error("expected Complete after last stage tool called")
	}
	if notified != 1 {
		t.Errorf("expected 1 notification, got %d", notified)
	}
}

func TestMaybeAdvanceStageNotInStage(t *testing.T) {
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
	// calling a tool not in the current stage should not advance
	p.maybeAdvanceStage("folio.write")
	if p.pavedPathState.CurrentStage != 0 {
		t.Errorf("expected CurrentStage 0, got %d", p.pavedPathState.CurrentStage)
	}
}

func TestMaybeAdvanceStageAlreadyComplete(t *testing.T) {
	notified := 0
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
			CalledTools:  make(map[string]bool),
		},
		notifier: func(msg *jsonrpc.Message) error {
			notified++
			return nil
		},
	}
	p.maybeAdvanceStage("folio.read")
	if notified != 0 {
		t.Errorf("expected no notification when already complete, got %d", notified)
	}
}

func TestHandlePavedPathsStatus(t *testing.T) {
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{{
			Name: "onboarding",
			Stages: []config.PavedPathStage{
				{Label: "orient", Tools: []string{"folio.read", "folio.glob"}},
			},
		}},
		pavedPathState: &pavedPathState{
			SelectedPath: "onboarding",
			CurrentStage: 0,
			CalledTools:  map[string]bool{"folio.read": true},
		},
	}
	result := p.HandlePavedPaths(nil)
	if !strings.Contains(result, "onboarding") {
		t.Errorf("expected path name: %s", result)
	}
	if !strings.Contains(result, "orient") {
		t.Errorf("expected stage label: %s", result)
	}
	if !strings.Contains(result, "folio.read") {
		t.Errorf("expected called tool: %s", result)
	}
	if !strings.Contains(result, "folio.glob") {
		t.Errorf("expected pending tool: %s", result)
	}
}
