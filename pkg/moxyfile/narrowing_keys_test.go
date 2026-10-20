package moxyfile

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseAllowToolsDistinguishesUnsetFromEmpty(t *testing.T) {
	unset, err := Parse([]byte(`disable-moxins = ["chix"]`))
	if err != nil {
		t.Fatal(err)
	}
	if unset.AllowTools != nil {
		t.Errorf("unset allow-tools must be nil, got %#v", unset.AllowTools)
	}
	if unset.BuildAllowToolSet().Restricted() {
		t.Error("unset allow-tools must be unrestricted")
	}

	empty, err := Parse([]byte(`allow-tools = []`))
	if err != nil {
		t.Fatal(err)
	}
	if empty.AllowTools == nil {
		t.Error("an empty allow-tools list must be non-nil: it allows nothing")
	}
	set := empty.BuildAllowToolSet()
	if !set.Restricted() || set.Allows("grit", "status") || set.Allows("", "restart") {
		t.Error("an empty allow-tools list must allow nothing")
	}
}

func TestParseAllowToolsRejectsMalformedEntries(t *testing.T) {
	for _, src := range []string{
		`allow-tools = [""]`,
		`allow-tools = ["grit."]`,
		`allow-tools = [".status"]`,
		`allow-tools = ["grit.status.extra"]`,
	} {
		if _, err := Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), "allow-tools") {
			t.Errorf("%s: expected an allow-tools error, got %v", src, err)
		}
	}
}

func TestAllowToolSetAllows(t *testing.T) {
	set := Config{AllowTools: []string{"smith", "grit.status", "restart"}}.BuildAllowToolSet()
	cases := []struct {
		server, tool string
		want         bool
	}{
		{"smith", "issue-create", true},
		{"grit", "status", true},
		{"grit", "commit", false},
		{"folio", "read", false},
		{"", "restart", true},
		{"", "batch", false},
		// Bare names are one namespace: an entry allows the server of that
		// name and the meta tool of that name alike.
		{"", "smith", true},
		{"restart", "anything", true},
	}
	for _, c := range cases {
		if got := set.Allows(c.server, c.tool); got != c.want {
			t.Errorf("Allows(%q, %q) = %t, want %t", c.server, c.tool, got, c.want)
		}
	}
}

func TestIntersectAllowTools(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want []string
	}{
		{"identical lists", []string{"grit", "folio.read"}, []string{"folio.read", "grit"}, []string{"folio.read", "grit"}},
		{"a whole server narrowed to one tool", []string{"grit"}, []string{"grit.status"}, []string{"grit.status"}},
		{"the narrowing works in either order", []string{"grit.status"}, []string{"grit"}, []string{"grit.status"}},
		{"different tools of one server share nothing", []string{"grit.status"}, []string{"grit.commit"}, []string{}},
		{"disjoint lists allow nothing", []string{"grit"}, []string{"folio"}, []string{}},
		{"an empty list empties the result", []string{"grit"}, []string{}, []string{}},
		{"partial overlap", []string{"grit", "folio.read", "chix"}, []string{"grit.status", "folio", "man"}, []string{"folio.read", "grit.status"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := IntersectAllowTools(c.a, c.b)
			if got == nil {
				t.Fatal("the intersection must never be nil")
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestMergeAllowToolsOnlyNarrowsDownTheHierarchy(t *testing.T) {
	global := Config{AllowTools: []string{"grit", "folio"}}
	middle := Config{}
	local := Config{AllowTools: []string{"grit.status", "chix"}}

	merged := Merge(Merge(Merge(Config{}, global), middle), local)
	if want := []string{"grit.status"}; !reflect.DeepEqual(merged.AllowTools, want) {
		t.Errorf("got %q, want %q", merged.AllowTools, want)
	}

	if got := Merge(Config{}, Config{}).AllowTools; got != nil {
		t.Errorf("no layer set allow-tools, so the merge must leave it unset; got %#v", got)
	}

	// A closer layer cannot lift an allowlist by leaving the key out.
	if got := Merge(global, Config{}).AllowTools; !reflect.DeepEqual(got, []string{"grit", "folio"}) {
		t.Errorf("got %q", got)
	}
}

func TestRenderEffectiveKeepsAResolvedMoxinsRecordEvenWhenEmpty(t *testing.T) {
	out := string(RenderEffective(Config{ResolvedMoxins: map[string]string{}}))
	if out != "[resolved-moxins]\n" {
		t.Errorf("got %q", out)
	}
	reparsed, err := ParseLiteral([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if reparsed.ResolvedMoxins == nil {
		t.Error("an empty record did not survive the round trip: it would read as no record")
	}

	withMoxins := Config{ResolvedMoxins: map[string]string{"zeta": "/m/zeta", "alpha": "/m/with \"quotes\""}}
	rendered := RenderEffective(withMoxins)
	want := "[resolved-moxins]\n\"alpha\" = \"/m/with \\\"quotes\\\"\"\n\"zeta\" = \"/m/zeta\"\n"
	if string(rendered) != want {
		t.Errorf("got %q, want %q", rendered, want)
	}
	back, err := ParseLiteral(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.ResolvedMoxins, withMoxins.ResolvedMoxins) {
		t.Errorf("round trip: got %v", back.ResolvedMoxins)
	}

	if got := RenderEffective(Config{}); len(got) != 0 {
		t.Errorf("no record must render nothing, got %q", got)
	}
}

func TestRenderEffectiveKeepsAnEmptyAllowList(t *testing.T) {
	out := string(RenderEffective(Config{AllowTools: []string{}}))
	if out != "allow-tools = []\n" {
		t.Errorf("got %q", out)
	}
	reparsed, err := ParseLiteral([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if reparsed.AllowTools == nil {
		t.Error("an empty allow-tools list did not survive the round trip")
	}

	sorted := string(RenderEffective(Config{AllowTools: []string{"grit.status", "folio", "folio"}}))
	if sorted != "allow-tools = [\"folio\", \"grit.status\"]\n" {
		t.Errorf("got %q", sorted)
	}
}
