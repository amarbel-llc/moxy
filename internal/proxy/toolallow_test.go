package proxy

import (
	"context"
	"sort"
	"strings"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/config"
	"code.linenisgreat.com/moxy/internal/naming"
	"code.linenisgreat.com/moxy/internal/toolfilter"
)

func allowSet(entries []string) config.AllowToolSet {
	return config.Config{AllowTools: entries}.BuildAllowToolSet()
}

func keptNames(tools []protocol.ToolV1) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	sort.Strings(names)
	return names
}

func TestApplyToolAllow(t *testing.T) {
	base := []protocol.ToolV1{
		{Name: "grit.status"},
		{Name: "folio.write"},
		{Name: "folio.read"},
		{Name: "restart"},
		{Name: "batch"},
	}
	cases := []struct {
		name  string
		allow []string
		want  []string
	}{
		{"no allowlist keeps everything", nil, []string{"batch", "folio.read", "folio.write", "grit.status", "restart"}},
		{"an empty allowlist keeps nothing", []string{}, []string{}},
		{"a bare name allows the whole server", []string{"folio"}, []string{"folio.read", "folio.write"}},
		{"a dotted name allows one tool", []string{"folio.read"}, []string{"folio.read"}},
		{"meta tools need their own bare name", []string{"grit", "restart"}, []string{"grit.status", "restart"}},
		{"a server name does not allow a meta tool", []string{"grit", "folio"}, []string{"folio.read", "folio.write", "grit.status"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &Proxy{
				toolFilter:   toolfilter.All(),
				toolAllow:    allowSet(c.allow),
				nameTemplate: naming.DefaultTemplate(),
			}
			got := keptNames(p.applyToolAllow(append([]protocol.ToolV1(nil), base...), naming.Registry{}))
			if len(got) != len(c.want) {
				t.Fatalf("allow %v: got %v, want %v", c.allow, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("allow %v: got %v, want %v", c.allow, got, c.want)
				}
			}
		})
	}
}

// allow-tools names tools the way the moxyfile does, "server.tool", whatever
// the advertised names look like.
func TestApplyToolAllowUsesCanonicalNamesUnderACustomTemplate(t *testing.T) {
	tmpl, err := naming.Parse("{server}_{tool}")
	if err != nil {
		t.Fatal(err)
	}
	b := naming.NewBuilder(tmpl)
	b.Add(naming.Entry{Server: "folio", Original: "read"})
	b.Add(naming.Entry{Server: "folio", Original: "write"})
	reg, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}

	p := &Proxy{
		toolFilter:   toolfilter.All(),
		toolAllow:    allowSet([]string{"folio.read"}),
		nameTemplate: tmpl,
	}
	got := keptNames(p.applyToolAllow([]protocol.ToolV1{
		{Name: "folio_read"},
		{Name: "folio_write"},
		{Name: "restart"},
	}, reg))
	if len(got) != 1 || got[0] != "folio_read" {
		t.Errorf("got %v, want [folio_read]", got)
	}
}

// Under a custom template the gate must judge the canonical name, not the
// rendered one, and must still refuse meta tools that are not listed.
func TestCallToolAllowGateUnderACustomTemplate(t *testing.T) {
	tmpl, err := naming.Parse("{server}_{tool}")
	if err != nil {
		t.Fatal(err)
	}
	b := naming.NewBuilder(tmpl)
	b.Add(naming.Entry{Server: "folio", Original: "read"})
	b.Add(naming.Entry{Server: "folio", Original: "write"})
	reg, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	p := &Proxy{
		toolFilter:   toolfilter.All(),
		toolAllow:    allowSet([]string{"folio.read"}),
		nameTemplate: tmpl,
		toolRegistry: reg,
	}
	// The listed tool passes the gate; with no child behind it, dispatch then
	// fails for another reason, which is all this asserts. It goes first:
	// an unknown name below makes the proxy re-list, and this bare Proxy has
	// no children to rebuild the registry from.
	res, err := p.callToolV1(context.Background(), "folio_read", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != nil && res.IsError && strings.Contains(res.Content[0].Text, "allow-tools") {
		t.Errorf("listed tool was refused by the allow gate: %+v", res)
	}
	for _, name := range []string{"folio_write", "restart"} {
		res, err := p.callToolV1(context.Background(), name, nil)
		if err != nil {
			t.Fatalf("callToolV1(%q) unexpected error: %v", name, err)
		}
		if res == nil || !res.IsError || !strings.Contains(res.Content[0].Text, "allow-tools") {
			t.Errorf("callToolV1(%q): expected an allow-tools refusal, got %+v", name, res)
		}
	}
}

// A tool that is not listed must be uncallable, not merely unlisted.
func TestCallToolAllowGate(t *testing.T) {
	p := &Proxy{
		toolFilter:   toolfilter.All(),
		toolAllow:    allowSet([]string{"folio.read"}),
		nameTemplate: naming.DefaultTemplate(),
	}
	for _, name := range []string{"grit.status", "folio.write", "restart", "batch"} {
		res, err := p.callToolV1(context.Background(), name, nil)
		if err != nil {
			t.Fatalf("callToolV1(%q) unexpected error: %v", name, err)
		}
		if res == nil || !res.IsError {
			t.Errorf("callToolV1(%q): expected an error result, got %+v", name, res)
		}
	}
}
