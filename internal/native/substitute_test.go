package native

import (
	"strings"
	"testing"
)

func TestSubstituteNoURIs(t *testing.T) {
	cache := newResultCache(t.TempDir())
	sub, err := substituteResultURIs("echo hello world", cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sub.Cleanup()

	if sub.Command != "echo hello world" {
		t.Errorf("command = %q, want %q", sub.Command, "echo hello world")
	}
	if len(sub.ExtraFiles) != 0 {
		t.Errorf("len(ExtraFiles) = %d, want 0", len(sub.ExtraFiles))
	}
}

func TestSubstituteSingleURI(t *testing.T) {
	cache := newResultCache(t.TempDir())
	if err := cache.store(cachedResult{
		ID:      "abc-123",
		Session: "sess",
		Output:  "cached content",
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	sub, err := substituteResultURIs(
		"grep pattern moxy.native://results/sess/abc-123",
		cache,
	)
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	defer sub.Cleanup()

	if strings.Contains(sub.Command, "moxy.native://") {
		t.Error("URI was not rewritten")
	}
	if !strings.Contains(sub.Command, "/dev/fd/3") {
		t.Errorf("expected /dev/fd/3 in command, got %q", sub.Command)
	}
	if sub.Command != "grep pattern /dev/fd/3" {
		t.Errorf("command = %q, want %q", sub.Command, "grep pattern /dev/fd/3")
	}
	if len(sub.ExtraFiles) != 1 {
		t.Errorf("len(ExtraFiles) = %d, want 1", len(sub.ExtraFiles))
	}
}

func TestSubstituteMultipleURIs(t *testing.T) {
	cache := newResultCache(t.TempDir())
	if err := cache.store(cachedResult{
		ID:      "aaa",
		Session: "sess",
		Output:  "first content",
	}); err != nil {
		t.Fatalf("store first: %v", err)
	}
	if err := cache.store(cachedResult{
		ID:      "bbb",
		Session: "sess",
		Output:  "second content",
	}); err != nil {
		t.Fatalf("store second: %v", err)
	}

	sub, err := substituteResultURIs(
		"diff moxy.native://results/sess/aaa moxy.native://results/sess/bbb",
		cache,
	)
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	defer sub.Cleanup()

	if sub.Command != "diff /dev/fd/3 /dev/fd/4" {
		t.Errorf("command = %q, want %q", sub.Command, "diff /dev/fd/3 /dev/fd/4")
	}
	if len(sub.ExtraFiles) != 2 {
		t.Errorf("len(ExtraFiles) = %d, want 2", len(sub.ExtraFiles))
	}
}

func TestSubstituteDuplicateURI(t *testing.T) {
	cache := newResultCache(t.TempDir())
	if err := cache.store(cachedResult{
		ID:      "dup",
		Session: "sess",
		Output:  "shared content",
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	sub, err := substituteResultURIs(
		"diff moxy.native://results/sess/dup moxy.native://results/sess/dup",
		cache,
	)
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	defer sub.Cleanup()

	if sub.Command != "diff /dev/fd/3 /dev/fd/3" {
		t.Errorf("command = %q, want %q", sub.Command, "diff /dev/fd/3 /dev/fd/3")
	}
	// Duplicate URI should reuse the same fd — only one ExtraFile.
	if len(sub.ExtraFiles) != 1 {
		t.Errorf("len(ExtraFiles) = %d, want 1 (should deduplicate)", len(sub.ExtraFiles))
	}
}

func TestSubstituteInvalidURI(t *testing.T) {
	cache := newResultCache(t.TempDir())
	// URI matches the regex pattern but has no cached data on disk.
	_, err := substituteResultURIs(
		"cat moxy.native://results/sess/nonexistent",
		cache,
	)
	if err == nil {
		t.Fatal("expected error for uncached URI, got nil")
	}
	if !strings.Contains(err.Error(), "loading") {
		t.Errorf("error = %q, expected it to mention 'loading'", err.Error())
	}
}
