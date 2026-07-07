package toolexclude

import (
	"sort"
	"testing"
)

func TestParseAndExcludes(t *testing.T) {
	cases := []struct {
		name    string
		names   []string
		server  string
		tool    string
		exclude bool
	}{
		{"whole server excludes its tool", []string{"chix"}, "chix", "chix.build", true},
		{"whole server does not exclude other server", []string{"chix"}, "folio", "folio.write", false},
		{"per-tool excludes exact name", []string{"folio.write"}, "folio", "folio.write", true},
		{"per-tool does not exclude sibling tool", []string{"folio.write"}, "folio", "folio.read", false},
		{"empty set excludes nothing", nil, "chix", "chix.build", false},
		{"empty server never matches a server entry", []string{"chix"}, "", "restart", false},
		{"whitespace-only entries are ignored", []string{"  ", ""}, "chix", "chix.build", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Parse(c.names)
			got := s.Excludes(c.server, c.tool)
			if got != c.exclude {
				t.Errorf("Parse(%v).Excludes(%q, %q) = %v, want %v", c.names, c.server, c.tool, got, c.exclude)
			}
		})
	}
}

func TestIsEmpty(t *testing.T) {
	if !Parse(nil).IsEmpty() {
		t.Error("Parse(nil) should be empty")
	}
	if !Parse([]string{}).IsEmpty() {
		t.Error("Parse([]string{}) should be empty")
	}
	if !Parse([]string{" ", ""}).IsEmpty() {
		t.Error("Parse of only-blank entries should be empty")
	}
	if Parse([]string{"chix"}).IsEmpty() {
		t.Error("Parse([\"chix\"]) should not be empty")
	}
	if Parse([]string{"folio.write"}).IsEmpty() {
		t.Error("Parse([\"folio.write\"]) should not be empty")
	}
}

func TestNames(t *testing.T) {
	s := Parse([]string{"chix", "folio.write", "folio.read"})
	got := s.Names()
	sort.Strings(got)
	want := []string{"chix", "folio.read", "folio.write"}
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names() = %v, want %v", got, want)
		}
	}
}

func TestNamesEmpty(t *testing.T) {
	if got := Parse(nil).Names(); got != nil {
		t.Errorf("Names() on empty Set = %v, want nil", got)
	}
}
