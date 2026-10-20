package perms

import "testing"

func TestPolicyZeroValueDelegates(t *testing.T) {
	var p Policy
	if p.Effective() != Delegate {
		t.Fatalf("Effective() = %q, want %q", p.Effective(), Delegate)
	}
	if p.Enforcing() {
		t.Fatal("zero Policy must not enforce")
	}
}

func TestPolicyNarrowerOnlyTightens(t *testing.T) {
	delegate := Policy{Mode: Delegate}
	enforce := Policy{Mode: Enforce}
	enforceGrantsFix := Policy{Mode: Enforce, Allow: []string{"fix"}}
	enforceGrantsEcho := Policy{Mode: Enforce, Allow: []string{"fix.echo"}}
	enforceGrantsOther := Policy{Mode: Enforce, Allow: []string{"other.tool"}}
	cases := []struct {
		name          string
		child, parent Policy
		want          bool
	}{
		{"delegate under delegate", delegate, delegate, true},
		{"enforce under delegate", enforce, delegate, true},
		{"enforce under enforce", enforce, enforce, true},
		{"delegate under enforce widens", delegate, enforce, false},
		{"zero under enforce widens", Policy{}, enforce, false},
		{"grant covered by parent's bare name", enforceGrantsEcho, enforceGrantsFix, true},
		{"grant the parent lacks widens", enforceGrantsOther, enforceGrantsFix, false},
		{"bare grant over parent's dotted grant widens", enforceGrantsFix, enforceGrantsEcho, false},
		{"any grant under a delegating parent is fine", enforceGrantsOther, delegate, true},
	}
	for _, c := range cases {
		if got := c.child.Narrower(c.parent); got != c.want {
			t.Errorf("%s: Narrower = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPolicyGrants(t *testing.T) {
	p := Policy{Mode: Enforce, Allow: []string{"fix", "other.tool"}}
	for name, want := range map[string]bool{
		"fix.echo":     true,
		"fix.anything": true,
		"other.tool":   true,
		"other.else":   false,
		"third.tool":   false,
	} {
		if got := p.Grants(name); got != want {
			t.Errorf("Grants(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestTightenEnforceWinsAndGrantsJoin(t *testing.T) {
	got := Tighten(Policy{Allow: []string{"a"}}, Policy{Mode: Enforce, Allow: []string{"b", "a"}})
	if !got.Enforcing() {
		t.Fatal("Tighten(delegate, enforce) must enforce")
	}
	if len(got.Allow) != 2 || got.Allow[0] != "a" || got.Allow[1] != "b" {
		t.Fatalf("Allow = %v, want [a b]", got.Allow)
	}
	if Tighten(Policy{}, Policy{}).Enforcing() {
		t.Fatal("Tighten(zero, zero) must delegate")
	}
}

func TestParseMode(t *testing.T) {
	for _, s := range []string{"", "delegate"} {
		if m, ok := ParseMode(s); !ok || m != Delegate {
			t.Errorf("ParseMode(%q) = %q, %v; want delegate, true", s, m, ok)
		}
	}
	if m, ok := ParseMode("enforce"); !ok || m != Enforce {
		t.Errorf("ParseMode(enforce) = %q, %v", m, ok)
	}
	if _, ok := ParseMode("ask"); ok {
		t.Error("ParseMode(ask) must be rejected")
	}
}
