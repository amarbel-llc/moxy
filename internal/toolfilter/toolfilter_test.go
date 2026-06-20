package toolfilter

import "testing"

func TestCategorize(t *testing.T) {
	cases := []struct {
		name string
		want Category
	}{
		{"restart", Meta},
		{"status", Meta},
		{"async-result", Meta},
		{"madder-mcp.resource-read", ResourceBridge},
		{"cutting-garden.resource-templates", ResourceBridge},
		{"grit.status", Child},
		{"just-us-agents.list-recipes", Child},
		{"foo.bar.baz", Child},
		// A failed-server status tool is <name>.status — a child-namespaced
		// name, not a builtin.
		{"nebulous.status", Child},
	}
	for _, c := range cases {
		if got := Categorize(c.name); got != c.want {
			t.Errorf("Categorize(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestParseProfiles(t *testing.T) {
	cases := []struct {
		spec  string
		child bool
		rb    bool
		meta  bool
	}{
		{"", true, true, true},                  // unset -> full
		{"full", true, true, true},              // explicit full
		{"no-meta", true, true, false},          // drop control surface
		{"resources-only", false, false, false}, // zero tools
		{"-meta", true, true, false},            // toggle off meta
		{"-meta,-resource-bridge", true, false, false},
		{"resources-only,+child", true, false, false}, // profile then re-add child
		{"no-meta,+meta", true, true, true},           // degenerate: back to full
		{" full , -meta ", true, true, false},         // whitespace tolerant
	}
	for _, c := range cases {
		f, err := Parse(c.spec)
		if err != nil {
			t.Fatalf("Parse(%q) unexpected error: %v", c.spec, err)
		}
		if f.Allows(Child) != c.child || f.Allows(ResourceBridge) != c.rb || f.Allows(Meta) != c.meta {
			t.Errorf("Parse(%q) = {child:%v rb:%v meta:%v}, want {child:%v rb:%v meta:%v}",
				c.spec,
				f.Allows(Child), f.Allows(ResourceBridge), f.Allows(Meta),
				c.child, c.rb, c.meta)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, spec := range []string{"bogus", "+nonsense", "-child,unknownprofile", "resources-only,+typo"} {
		if _, err := Parse(spec); err == nil {
			t.Errorf("Parse(%q) expected error, got nil", spec)
		}
	}
}

func TestAllAllowsEverything(t *testing.T) {
	f := All()
	for _, c := range []Category{Child, ResourceBridge, Meta} {
		if !f.Allows(c) {
			t.Errorf("All() should allow %v", c)
		}
	}
}

func TestFilterString(t *testing.T) {
	if got := All().String(); got != "child,resource-bridge,meta" {
		t.Errorf("All().String() = %q", got)
	}
	resourcesOnly, _ := Parse("resources-only")
	if got := resourcesOnly.String(); got != "none" {
		t.Errorf("resources-only String() = %q, want none", got)
	}
}
