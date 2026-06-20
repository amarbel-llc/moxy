package proxy

import (
	"strings"
	"testing"
)

func TestFormatSystemPromptFragmentEmpty(t *testing.T) {
	frag, ok := FormatSystemPromptFragment(nil)
	if ok || frag != "" {
		t.Errorf("empty summaries: want (\"\", false), got (%q, %v)", frag, ok)
	}
}

func TestFormatSystemPromptFragmentRunning(t *testing.T) {
	frag, ok := FormatSystemPromptFragment([]ServerSummary{
		{Name: "chrest", Status: "running", Tools: 19},
		{Name: "arboretum", Status: "running", Tools: 6},
	})
	if !ok {
		t.Fatal("want ok=true for running servers")
	}
	for _, want := range []string{
		"Connected:", "chrest (19 tools)", "arboretum (6 tools)",
		"moxy://tools/{server}", "madder://blobs",
	} {
		if !strings.Contains(frag, want) {
			t.Errorf("fragment missing %q:\n%s", want, frag)
		}
	}
}

// The most valuable dynamic content: failed children, otherwise invisible to
// the agent until a tool call fails.
func TestFormatSystemPromptFragmentFailed(t *testing.T) {
	frag, ok := FormatSystemPromptFragment([]ServerSummary{
		{Name: "chrest", Status: "running", Tools: 19},
		{Name: "nebulous", Status: "failed", Error: "child process nebulous exited unexpectedly"},
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	for _, want := range []string{
		"Failed to start", "nebulous", "exited unexpectedly", "chrest (19 tools)",
	} {
		if !strings.Contains(frag, want) {
			t.Errorf("fragment missing %q:\n%s", want, frag)
		}
	}
}
