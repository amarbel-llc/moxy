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
	}
	for _, c := range cases {
		if got := c.child.Narrower(c.parent); got != c.want {
			t.Errorf("%s: Narrower = %v, want %v", c.name, got, c.want)
		}
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
