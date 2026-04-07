package main

import (
	"strings"
	"testing"
)

const testSession = "test-session"

func storeFixture(t *testing.T, cache *execResultCache, id, output string) {
	t.Helper()
	if err := cache.store(cachedExecResult{
		ID:      id,
		Session: testSession,
		Command: "fixture",
		Output:  output,
	}); err != nil {
		t.Fatalf("store fixture %s: %v", id, err)
	}
}

func fixtureURI(id string) string {
	return "maneater.exec://results/" + testSession + "/" + id
}

func TestSubstituteExecURIsNoMatch(t *testing.T) {
	cache := &execResultCache{dir: t.TempDir()}
	sub, err := substituteExecURIs("ls -la /tmp", cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sub.Cleanup()
	if sub.Command != "ls -la /tmp" {
		t.Errorf("Command = %q, want unchanged", sub.Command)
	}
	if len(sub.ExtraFiles) != 0 {
		t.Errorf("ExtraFiles len = %d, want 0", len(sub.ExtraFiles))
	}
}

func TestSubstituteExecURIsSingle(t *testing.T) {
	cache := &execResultCache{dir: t.TempDir()}
	storeFixture(t, cache, "abc-1", "hello\nworld\n")

	sub, err := substituteExecURIs("wc -l "+fixtureURI("abc-1"), cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sub.Cleanup()

	if sub.Command != "wc -l /dev/fd/3" {
		t.Errorf("Command = %q, want %q", sub.Command, "wc -l /dev/fd/3")
	}
	if len(sub.ExtraFiles) != 1 {
		t.Errorf("ExtraFiles len = %d, want 1", len(sub.ExtraFiles))
	}
}

func TestSubstituteExecURIsTwoDistinct(t *testing.T) {
	cache := &execResultCache{dir: t.TempDir()}
	storeFixture(t, cache, "id-a", "a\n")
	storeFixture(t, cache, "id-b", "b\n")

	cmd := "diff " + fixtureURI("id-a") + " " + fixtureURI("id-b")
	sub, err := substituteExecURIs(cmd, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sub.Cleanup()

	want := "diff /dev/fd/3 /dev/fd/4"
	if sub.Command != want {
		t.Errorf("Command = %q, want %q", sub.Command, want)
	}
	if len(sub.ExtraFiles) != 2 {
		t.Errorf("ExtraFiles len = %d, want 2", len(sub.ExtraFiles))
	}
}

func TestSubstituteExecURIsRepeatedSameID(t *testing.T) {
	cache := &execResultCache{dir: t.TempDir()}
	storeFixture(t, cache, "shared", "x\n")

	cmd := "diff " + fixtureURI("shared") + " " + fixtureURI("shared")
	sub, err := substituteExecURIs(cmd, cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sub.Cleanup()

	// Both occurrences must point to the same fd; only one ExtraFile.
	if !strings.Contains(sub.Command, "/dev/fd/3") {
		t.Errorf("Command = %q, missing /dev/fd/3", sub.Command)
	}
	if strings.Contains(sub.Command, "/dev/fd/4") {
		t.Errorf("Command = %q, unexpected /dev/fd/4 (should reuse fd 3)", sub.Command)
	}
	if got := strings.Count(sub.Command, "/dev/fd/3"); got != 2 {
		t.Errorf("/dev/fd/3 count = %d, want 2", got)
	}
	if len(sub.ExtraFiles) != 1 {
		t.Errorf("ExtraFiles len = %d, want 1", len(sub.ExtraFiles))
	}
}

func TestSubstituteExecURIsMissingID(t *testing.T) {
	cache := &execResultCache{dir: t.TempDir()}
	_, err := substituteExecURIs("cat "+fixtureURI("nope"), cache)
	if err == nil {
		t.Fatal("expected error for missing id")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error %q does not name the offending URI", err.Error())
	}
}
