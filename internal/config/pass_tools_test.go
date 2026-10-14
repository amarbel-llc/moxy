package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePassTools(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr string
	}{
		{
			name: "pass tools beside an allowlist",
			src:  "allow-tools = [\"grit.status\"]\npass-tools = [\"smith.issue-create\", \"chix\"]\n",
		},
		{
			// The holder calls grit.status and may confer the rest of grit.
			name: "a bare pass entry over a dotted allow entry",
			src:  "allow-tools = [\"grit.status\"]\npass-tools = [\"grit\"]\n",
		},
		{
			name: "an empty pass list beside an allowlist",
			src:  "allow-tools = []\npass-tools = []\n",
		},
		{
			// An empty list holds nothing at pass, as an unset key does.
			name: "an empty pass list without an allowlist",
			src:  `pass-tools = []`,
		},
		{
			// With no allowlist every tool is usable, so a pass tool would be
			// callable by its holder.
			name:    "pass tools without an allowlist",
			src:     `pass-tools = ["smith.issue-create"]`,
			wantErr: "pass-tools is set without allow-tools",
		},
		{
			name:    "a tool listed at both levels",
			src:     "allow-tools = [\"smith.issue-create\"]\npass-tools = [\"smith.issue-create\"]\n",
			wantErr: `pass-tools: "smith.issue-create" is also allowed by allow-tools`,
		},
		{
			name:    "a pass tool covered by a bare allow entry",
			src:     "allow-tools = [\"smith\"]\npass-tools = [\"smith.issue-create\"]\n",
			wantErr: `pass-tools: "smith.issue-create" is also allowed by allow-tools`,
		},
		{
			name:    "a malformed pass entry",
			src:     "allow-tools = []\npass-tools = [\"grit.\"]\n",
			wantErr: "pass-tools: invalid entry",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseLiteral([]byte(c.src))
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
				t.Fatalf("expected an error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

// Each layer that sets allow-tools holds every tool at one of three levels
// (none < pass < use); the merge keeps, per tool, the lowest level any such
// layer holds it at. A layer without allow-tools says nothing about tools.
func TestMergePassToolsTakesTheLowestLevelPerTool(t *testing.T) {
	cases := []struct {
		name                string
		layers              []Config
		wantAllow, wantPass []string
	}{
		{
			name:      "no layer sets pass-tools",
			layers:    []Config{{AllowTools: []string{"one"}}},
			wantAllow: []string{"one"},
		},
		{
			name: "a closer layer cannot lift pass to use",
			layers: []Config{
				{AllowTools: []string{"one"}, PassTools: []string{"two"}},
				{AllowTools: []string{"one", "two"}},
			},
			wantAllow: []string{"one"},
			wantPass:  []string{"two"},
		},
		{
			name: "a closer layer may lower use to pass",
			layers: []Config{
				{AllowTools: []string{"one", "two"}},
				{AllowTools: []string{"one"}, PassTools: []string{"two"}},
			},
			wantAllow: []string{"one"},
			wantPass:  []string{"two"},
		},
		{
			name: "a closer allowlist without pass-tools drops the pass tools",
			layers: []Config{
				{AllowTools: []string{"one"}, PassTools: []string{"two"}},
				{AllowTools: []string{"one"}},
			},
			wantAllow: []string{"one"},
		},
		{
			name: "a closer layer cannot pass what a farther one holds at none",
			layers: []Config{
				{AllowTools: []string{"one"}},
				{AllowTools: []string{"one"}, PassTools: []string{"two"}},
			},
			wantAllow: []string{"one"},
		},
		{
			name: "a layer without allow-tools leaves both lists alone",
			layers: []Config{
				{AllowTools: []string{"one"}, PassTools: []string{"two"}},
				{},
			},
			wantAllow: []string{"one"},
			wantPass:  []string{"two"},
		},
		{
			name: "the first layer to set an allowlist establishes both lists",
			layers: []Config{
				{},
				{AllowTools: []string{"one"}, PassTools: []string{"two"}},
			},
			wantAllow: []string{"one"},
			wantPass:  []string{"two"},
		},
		{
			name: "levels are judged per tool under bare and dotted entries",
			layers: []Config{
				{AllowTools: []string{"grit"}},
				{AllowTools: []string{"grit.status"}, PassTools: []string{"grit"}},
			},
			wantAllow: []string{"grit.status"},
			wantPass:  []string{"grit"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var merged Config
			for _, layer := range c.layers {
				merged = Merge(merged, layer)
			}
			if !reflect.DeepEqual(merged.AllowTools, c.wantAllow) {
				t.Errorf("allow-tools: got %q, want %q", merged.AllowTools, c.wantAllow)
			}
			if !reflect.DeepEqual(merged.PassTools, c.wantPass) {
				t.Errorf("pass-tools: got %#v, want %#v", merged.PassTools, c.wantPass)
			}
		})
	}
}

func TestRenderEffectiveWritesPassToolsSortedAndRoundTrips(t *testing.T) {
	cfg := Config{AllowTools: []string{"one"}, PassTools: []string{"two", "chix", "two"}}
	out := string(RenderEffective(cfg))
	want := "allow-tools = [\"one\"]\npass-tools = [\"chix\", \"two\"]\n"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
	back, err := ParseLiteral([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.PassTools, []string{"chix", "two"}) {
		t.Errorf("round trip: got %q", back.PassTools)
	}

	if got := string(RenderEffective(Config{AllowTools: []string{"one"}, PassTools: []string{}})); got != "allow-tools = [\"one\"]\n" {
		t.Errorf("an empty pass list holds nothing and renders nothing, got %q", got)
	}
}
