package proxy

import (
	"context"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/naming"
	"code.linenisgreat.com/moxy/internal/toolexclude"
	"code.linenisgreat.com/moxy/internal/toolfilter"
)

func TestApplyToolExclude(t *testing.T) {
	base := []protocol.ToolV1{
		{Name: "grit.status"},
		{Name: "folio.write"},
		{Name: "folio.read"},
		{Name: "restart"},
	}
	cases := []struct {
		name    string
		exclude []string
		want    []string
	}{
		{"empty exclude keeps everything", nil, []string{"grit.status", "folio.write", "folio.read", "restart"}},
		{"whole-server exclusion drops every tool of that server", []string{"grit"}, []string{"folio.write", "folio.read", "restart"}},
		{"per-tool exclusion drops only that tool", []string{"folio.write"}, []string{"grit.status", "folio.read", "restart"}},
		{"builtin excluded by bare name", []string{"restart"}, []string{"grit.status", "folio.write", "folio.read"}},
		{"combination", []string{"grit", "folio.write"}, []string{"folio.read", "restart"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &Proxy{
				toolFilter:   toolfilter.All(),
				toolExclude:  toolexclude.Parse(c.exclude),
				nameTemplate: naming.DefaultTemplate(),
			}
			cp := append([]protocol.ToolV1(nil), base...)
			got := toolNameSet(p.applyToolExclude(cp, naming.Registry{}))
			if len(got) != len(c.want) {
				t.Errorf("exclude %v: got %d tools %v, want %d %v", c.exclude, len(got), got, len(c.want), c.want)
			}
			for _, n := range c.want {
				if !got[n] {
					t.Errorf("exclude %v: expected %q to survive", c.exclude, n)
				}
			}
		})
	}
}

// An excluded name must be uncallable, not merely unlisted — mirrors
// TestCallToolFilterGate's assertion for the --expose category filter.
func TestCallToolExcludeGate(t *testing.T) {
	p := &Proxy{
		toolFilter:   toolfilter.All(),
		toolExclude:  toolexclude.Parse([]string{"grit", "folio.write"}),
		nameTemplate: naming.DefaultTemplate(),
	}

	for _, name := range []string{"grit.status", "folio.write"} {
		res, err := p.callToolV1(context.Background(), name, nil)
		if err != nil {
			t.Fatalf("callToolV1(%q) unexpected error: %v", name, err)
		}
		if res == nil || !res.IsError {
			t.Errorf("callToolV1(%q) excluded: expected error result, got %+v", name, res)
		}
	}
}

func TestSetToolExcludeNotifiesOnChange(t *testing.T) {
	var notified int
	p := &Proxy{
		toolFilter:   toolfilter.All(),
		nameTemplate: naming.DefaultTemplate(),
	}
	p.notifier = func(_ *jsonrpc.Message) error { notified++; return nil }

	p.SetToolExclude(toolexclude.Parse([]string{"chix"}))
	if notified != 1 {
		t.Errorf("first SetToolExclude with a new set: notified = %d, want 1", notified)
	}

	// Setting an equivalent set again must not spuriously notify.
	p.SetToolExclude(toolexclude.Parse([]string{"chix"}))
	if notified != 1 {
		t.Errorf("SetToolExclude with an unchanged set: notified = %d, want 1 (no new notification)", notified)
	}

	p.SetToolExclude(toolexclude.Parse([]string{"chix", "folio.write"}))
	if notified != 2 {
		t.Errorf("SetToolExclude with a genuinely different set: notified = %d, want 2", notified)
	}

	if got := p.ToolExclude().Names(); len(got) != 2 {
		t.Errorf("ToolExclude().Names() = %v, want 2 entries", got)
	}
}
