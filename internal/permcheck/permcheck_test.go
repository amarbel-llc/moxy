package permcheck

import (
	"context"
	"encoding/json"
	"testing"

	"code.linenisgreat.com/moxy/internal/native"
)

func TestNewResolver_NoMoxins(t *testing.T) {
	// Use a non-existent MOXIN_PATH (not "") so resolveMoxinDirs skips the
	// CWD-hierarchy fallback that would otherwise pick up ambient
	// .moxy/moxins directories.
	t.Setenv("MOXIN_PATH", t.TempDir()+"/nonexistent")
	r, err := NewResolver()
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	dec, _ := r.Resolve(context.Background(), "anything.tool", nil, ".")
	if dec != Unknown {
		t.Fatalf("dec = %q, want %q", dec, Unknown)
	}
}

func TestResolve_AlwaysAllow(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-allow")
	r, err := NewResolver()
	if err != nil {
		t.Fatal(err)
	}
	dec, reason := r.Resolve(context.Background(), "allow-srv.echo", nil, ".")
	if dec != Allow {
		t.Fatalf("dec = %q, want %q (reason: %s)", dec, Allow, reason)
	}
	if reason == "" {
		t.Error("reason must be non-empty for Allow")
	}
}

func TestResolve_EachUse(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-each")
	r, err := NewResolver()
	if err != nil {
		t.Fatal(err)
	}
	dec, reason := r.Resolve(context.Background(), "each-srv.echo", nil, ".")
	if dec != Ask {
		t.Fatalf("dec = %q, want %q", dec, Ask)
	}
	if reason == "" {
		t.Error("reason must be non-empty for Ask")
	}
}

func TestResolve_DynamicAllow(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic-allow")
	r, err := NewResolver()
	if err != nil {
		t.Fatal(err)
	}
	dec, reason := r.Resolve(
		context.Background(),
		"dynallow-srv.echo",
		json.RawMessage(`{}`),
		".",
	)
	if dec != Allow {
		t.Fatalf("dec = %q, want %q (reason: %s)", dec, Allow, reason)
	}
}

func TestResolve_DynamicDeny(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic-deny")
	r, err := NewResolver()
	if err != nil {
		t.Fatal(err)
	}
	dec, reason := r.Resolve(
		context.Background(),
		"dyndeny-srv.echo",
		json.RawMessage(`{}`),
		".",
	)
	if dec != Deny {
		t.Fatalf("dec = %q, want %q (reason: %s)", dec, Deny, reason)
	}
}

// TestResolve_DynamicAsk exercises the DynPermsAsk arm of evalDynamic, which
// fires when the dynamic-perms script exits with code 1. Without this case,
// evalDynamic falls below the 85% coverage target.
func TestResolve_DynamicAsk(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic-ask")
	r, err := NewResolver()
	if err != nil {
		t.Fatal(err)
	}
	dec, reason := r.Resolve(
		context.Background(),
		"dynask-srv.echo",
		json.RawMessage(`{}`),
		".",
	)
	if dec != Ask {
		t.Fatalf("dec = %q, want %q (reason: %s)", dec, Ask, reason)
	}
}

func TestResolve_UnknownTool(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-allow")
	r, err := NewResolver()
	if err != nil {
		t.Fatal(err)
	}
	dec, _ := r.Resolve(context.Background(), "missing-srv.tool", nil, ".")
	if dec != Unknown {
		t.Fatalf("dec = %q, want %q", dec, Unknown)
	}
}

func TestResolve_DynamicNilSpec(t *testing.T) {
	r := &Resolver{
		perms: map[string]ToolPermInfo{
			"bad.tool": {Perm: native.PermsDynamic, DynamicPerms: nil},
		},
	}
	dec, _ := r.Resolve(context.Background(), "bad.tool", nil, ".")
	if dec != Unknown {
		t.Fatalf("dec = %q, want %q (defensive nil-spec branch)", dec, Unknown)
	}
}

func TestResolve_DelegateToClient(t *testing.T) {
	r := &Resolver{
		perms: map[string]ToolPermInfo{
			"srv.tool": {Perm: native.PermsDelegateToClient},
		},
	}
	dec, reason := r.Resolve(context.Background(), "srv.tool", nil, ".")
	if dec != Unknown {
		t.Fatalf("dec = %q, want Unknown (reason: %s)", dec, reason)
	}
}
