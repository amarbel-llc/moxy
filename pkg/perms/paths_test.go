package perms

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"code.linenisgreat.com/moxy/internal/native"
)

func TestPassThroughFormsAreExact(t *testing.T) {
	for _, s := range []string{"/dev/fd/3", "/dev/fd/63"} {
		if !IsDescriptorPath(s) {
			t.Errorf("IsDescriptorPath(%q) = false", s)
		}
	}
	for _, s := range []string{"/dev/fd/", "/dev/fd/x", "/dev/fd/3/..", "/dev/fd/../../etc/passwd", "/dev/fd/3 ", "dev/fd/3"} {
		if IsDescriptorPath(s) {
			t.Errorf("IsDescriptorPath(%q) = true", s)
		}
	}
	if !IsBlobURI("madder://blobs/blake2b256-abc123") {
		t.Error("a plain blob URI is not recognised")
	}
	for _, s := range []string{"madder://blobs/", "madder://blobs/.", "madder://blobs/..", "madder://blobs/a/b", "madder://blobs/a/../../etc", "madder://blobs/a b"} {
		if IsBlobURI(s) {
			t.Errorf("IsBlobURI(%q) = true", s)
		}
	}
}

// A value that only starts like a pass-through form names a file, and is
// asked about as one.
func TestResourcesOfLookalikePassThroughIsAFile(t *testing.T) {
	dir := t.TempDir()
	specs := []native.ResourceArg{{Param: "p", Op: "write"}}
	for _, v := range []string{"/dev/fd/../../etc/passwd", "madder://blobs/a/../../../x"} {
		args, _ := json.Marshal(map[string]string{"p": v})
		got := ResourcesOf(specs, args, dir)
		if len(got) != 1 || got[0].Resource.Kind != KindFile {
			t.Errorf("ResourcesOf(%q) = %v, want one file resource", v, got)
		}
	}
}

// ".." after a name that is there but is no directory (a pipe behind a
// /proc link, a dangling link, a file) has no text meaning: the system fails
// there, so the rest stays as written and the open fails too.
func TestCanonicalPathNeverClimbsOutOfWhatItCannotResolve(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("pipe:[1]", filepath.Join(root, "pipe")); err != nil {
		t.Fatal(err)
	}
	for _, rest := range []string{"pipe/../x", "pipe/../../etc/passwd", "file/../x", "pipe/sub/../../x", "pipe/..", "file/."} {
		in := root + "/" + rest
		got := CanonicalPath(in)
		if got != in {
			t.Errorf("CanonicalPath(%q) = %q, want it unchanged", in, got)
		}
		if _, err := os.Stat(got); err == nil {
			t.Errorf("CanonicalPath(%q) = %q, which opens", in, got)
		}
	}
	// a name that is simply missing is no link: ".." applies to it
	if got, want := CanonicalPath(root+"/new/../file"), root+"/file"; got != want {
		t.Errorf("CanonicalPath through a missing name = %q, want %q", got, want)
	}
}

// The judged path is the one the system would open: ".." applies to what a
// link points at, not to the link's name, and a final "..", "." or slash is
// resolved too.
func TestCanonicalPathResolvesDotDotPhysically(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"wt/sub", "out/dir"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "out", "dir"), filepath.Join(root, "wt", "link")); err != nil {
		t.Fatal(err)
	}
	wt := root + "/wt"
	for in, want := range map[string]string{
		wt + "/link/../f":     root + "/out/f",
		wt + "/link/..":       root + "/out",
		wt + "/link/":         root + "/out/dir",
		wt + "/link/.":        root + "/out/dir",
		wt + "/link":          wt + "/link", // a plain link leaf is not followed
		wt + "/sub/..":        wt,
		wt + "/..":            root,
		wt + "/sub/../new/f":  wt + "/new/f",
		wt + "/new/../sub/f":  wt + "/sub/f",
		wt + "/./sub//./file": wt + "/sub/file",
	} {
		if got := CanonicalPath(in); got != want {
			t.Errorf("CanonicalPath(%q) = %q, want %q", in, got, want)
		}
	}
	// the same for a relative resource: joining it to the cwd must not
	// shorten ".." as text first
	args, _ := json.Marshal(map[string]string{"p": "link/../f"})
	got := ResourcesOf([]native.ResourceArg{{Param: "p", Op: "write"}}, args, wt)
	if want := "file://" + root + "/out/f"; len(got) != 1 || got[0].Resource.URI != want {
		t.Errorf("ResourcesOf = %v, want URI %s", got, want)
	}
}
