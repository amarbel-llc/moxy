package perms

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"code.linenisgreat.com/moxy/internal/native"
)

// fakeGate answers from a table keyed by resource URI; a URI absent from the
// table is "no answer".
type fakeGate struct {
	verdicts map[string]Decision
	asked    []string
}

func (g *fakeGate) Check(_ context.Context, op Op, res Resource) (Decision, string, bool) {
	g.asked = append(g.asked, string(op)+" "+res.Kind+" "+res.URI)
	d, ok := g.verdicts[res.URI]
	return d, "fake " + res.URI, ok
}

func TestResourcesOf(t *testing.T) {
	dir := t.TempDir()
	specs := []native.ResourceArg{
		{Param: "file_path", Op: "read"},
		{Param: "paths", Op: "write", Split: true},
		{Param: "list", Op: "read"},
	}
	args := json.RawMessage(`{
		"file_path": "rel/a.txt",
		"paths": "/abs/x /abs/y  ",
		"list": ["madder://blobs/blake2b256-abc", "/dev/fd/63", ""]
	}`)
	got := ResourcesOf(specs, args, dir)
	want := []OpResource{
		{Op: Read, Resource: Resource{Kind: KindFile, URI: "file://" + filepath.Join(dir, "rel/a.txt")}},
		{Op: Write, Resource: Resource{Kind: KindFile, URI: "file:///abs/x"}},
		{Op: Write, Resource: Resource{Kind: KindFile, URI: "file:///abs/y"}},
		{Op: Read, Resource: Resource{Kind: KindBlob, URI: "madder://blobs/blake2b256-abc"}},
	}
	if len(got) != len(want) {
		t.Fatalf("ResourcesOf = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ResourcesOf[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestResourcesOfResolvesParentSymlinkNotLeaf(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent-target", filepath.Join(real, "leaf")); err != nil {
		t.Fatal(err)
	}
	// real dir resolved through the parent symlink; the leaf symlink kept.
	got := ResourcesOf([]native.ResourceArg{{Param: "p", Op: "read"}},
		json.RawMessage(`{"p":"`+filepath.Join(root, "link", "leaf")+`"}`), root)
	want := "file://" + filepath.Join(real, "leaf")
	if len(got) != 1 || got[0].Resource.URI != want {
		t.Fatalf("ResourcesOf = %v, want URI %s", got, want)
	}
	// a path under a missing directory still resolves its existing ancestor.
	got = ResourcesOf([]native.ResourceArg{{Param: "p", Op: "write"}},
		json.RawMessage(`{"p":"`+filepath.Join(root, "link", "new", "f")+`"}`), root)
	want = "file://" + filepath.Join(real, "new", "f")
	if len(got) != 1 || got[0].Resource.URI != want {
		t.Fatalf("ResourcesOf = %v, want URI %s", got, want)
	}
}

func dynamicResolver(t *testing.T, floorExit string, gate Gate) *Resolver {
	t.Helper()
	spec := &native.DynamicPermsSpec{
		Command:  "bash",
		Args:     []string{"-c", "exit " + floorExit, "floor"},
		ArgOrder: []string{"p"},
	}
	r := NewResolverWithPerms(map[string]ToolPermInfo{
		"srv.t": {
			Perm:         native.PermsDynamic,
			DynamicPerms: spec,
			Resources:    []native.ResourceArg{{Param: "p", Op: "read"}},
		},
	})
	r.SetGate(gate)
	return r
}

func resolve(r *Resolver, path string) (Decision, string) {
	return r.Resolve(context.Background(), "srv.t", json.RawMessage(`{"p":"`+path+`"}`), "/")
}

func TestResolveGateVerdictIsFinalEvenAgainstTheFloor(t *testing.T) {
	// The floor would deny (exit 2); the broker allowed: the verdict wins.
	g := &fakeGate{verdicts: map[string]Decision{"file:///x": Allow}}
	if dec, _ := resolve(dynamicResolver(t, "2", g), "/x"); dec != Allow {
		t.Errorf("dec = %q, want allow", dec)
	}
	// The floor would allow (exit 0); the broker denied.
	g = &fakeGate{verdicts: map[string]Decision{"file:///x": Deny}}
	if dec, _ := resolve(dynamicResolver(t, "0", g), "/x"); dec != Deny {
		t.Errorf("dec = %q, want deny", dec)
	}
	g = &fakeGate{verdicts: map[string]Decision{"file:///x": Ask}}
	if dec, _ := resolve(dynamicResolver(t, "0", g), "/x"); dec != Ask {
		t.Errorf("dec = %q, want ask", dec)
	}
}

func TestResolveNoAnswerFallsToTheFloor(t *testing.T) {
	g := &fakeGate{verdicts: map[string]Decision{}}
	for floor, want := range map[string]Decision{"0": Allow, "1": Ask, "2": Deny} {
		if dec, _ := resolve(dynamicResolver(t, floor, g), "/x"); dec != want {
			t.Errorf("floor exit %s: dec = %q, want %q", floor, dec, want)
		}
	}
}

func TestResolveWithoutGateIsTheFloor(t *testing.T) {
	r := dynamicResolver(t, "1", nil)
	if dec, _ := resolve(r, "/x"); dec != Ask {
		t.Errorf("dec = %q, want ask", dec)
	}
}

func TestResolvePartialAnswerNeverWidensTheFloor(t *testing.T) {
	spec := &native.DynamicPermsSpec{Command: "bash", Args: []string{"-c", "exit 0", "floor"}, ArgOrder: []string{"a", "b"}}
	mk := func(g Gate) *Resolver {
		r := NewResolverWithPerms(map[string]ToolPermInfo{"srv.t": {
			Perm: native.PermsDynamic, DynamicPerms: spec,
			Resources: []native.ResourceArg{{Param: "a", Op: "read"}, {Param: "b", Op: "write"}},
		}})
		r.SetGate(g)
		return r
	}
	args := json.RawMessage(`{"a":"/a","b":"/b"}`)
	// a is answered ask, b is unanswered and the floor allows: ask stands.
	g := &fakeGate{verdicts: map[string]Decision{"file:///a": Ask}}
	if dec, _ := mk(g).Resolve(context.Background(), "srv.t", args, "/"); dec != Ask {
		t.Errorf("dec = %q, want ask", dec)
	}
	// a is answered allow, b is unanswered and the floor allows: allow.
	g = &fakeGate{verdicts: map[string]Decision{"file:///a": Allow}}
	if dec, _ := mk(g).Resolve(context.Background(), "srv.t", args, "/"); dec != Allow {
		t.Errorf("dec = %q, want allow", dec)
	}
	// a is answered deny: deny, whatever the rest.
	g = &fakeGate{verdicts: map[string]Decision{"file:///a": Deny}}
	if dec, _ := mk(g).Resolve(context.Background(), "srv.t", args, "/"); dec != Deny {
		t.Errorf("dec = %q, want deny", dec)
	}
}

func TestResolveStrictestVerdictAcrossResourcesWins(t *testing.T) {
	spec := &native.DynamicPermsSpec{Command: "bash", Args: []string{"-c", "exit 0", "floor"}, ArgOrder: []string{"a", "b"}}
	r := NewResolverWithPerms(map[string]ToolPermInfo{"srv.t": {
		Perm: native.PermsDynamic, DynamicPerms: spec,
		Resources: []native.ResourceArg{{Param: "a", Op: "read"}, {Param: "b", Op: "write"}},
	}})
	r.SetGate(&fakeGate{verdicts: map[string]Decision{"file:///a": Allow, "file:///b": Ask}})
	dec, _ := r.Resolve(context.Background(), "srv.t", json.RawMessage(`{"a":"/a","b":"/b"}`), "/")
	if dec != Ask {
		t.Errorf("dec = %q, want ask", dec)
	}
}

func TestConsultGateOnlyReportsAnsweredVerdicts(t *testing.T) {
	g := &fakeGate{verdicts: map[string]Decision{"file:///x": Deny}}
	r := dynamicResolver(t, "0", g)
	dec, _, answered := r.ConsultGate(context.Background(), "srv.t", json.RawMessage(`{"p":"/x"}`), "/")
	if !answered || dec != Deny {
		t.Errorf("ConsultGate = (%q, %v), want (deny, true)", dec, answered)
	}
	_, _, answered = r.ConsultGate(context.Background(), "srv.t", json.RawMessage(`{"p":"/other"}`), "/")
	if answered {
		t.Error("ConsultGate answered with no verdict from the gate")
	}
	_, _, answered = r.ConsultGate(context.Background(), "nope.t", nil, "/")
	if answered {
		t.Error("ConsultGate answered for an unknown tool")
	}
}

func TestGateOnlyConsultedForDynamicTier(t *testing.T) {
	g := &fakeGate{verdicts: map[string]Decision{"file:///x": Allow}}
	r := NewResolverWithPerms(map[string]ToolPermInfo{"srv.t": {
		Perm:      native.PermsEachUse,
		Resources: []native.ResourceArg{{Param: "p", Op: "read"}},
	}})
	r.SetGate(g)
	if dec, _ := r.Resolve(context.Background(), "srv.t", json.RawMessage(`{"p":"/x"}`), "/"); dec != Ask {
		t.Errorf("dec = %q, want ask: a broker allow must not lift each-use", dec)
	}
	if len(g.asked) != 0 {
		t.Errorf("gate consulted for a non-dynamic tier: %v", g.asked)
	}
}
