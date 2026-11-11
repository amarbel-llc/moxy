package readguard

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.linenisgreat.com/moxy/pkg/perms"
)

// policy allows paths under allow, asks about everything else, denies paths
// under deny, and records what it was asked.
type policy struct {
	allow, deny string
	asked       []string
}

func (p *policy) Decide(_ context.Context, path string) (perms.Decision, string) {
	p.asked = append(p.asked, path)
	switch {
	case p.deny != "" && strings.HasPrefix(path, p.deny):
		return perms.Deny, "on the deny list"
	case strings.HasPrefix(path, p.allow):
		return perms.Allow, "inside"
	}
	return perms.Ask, "outside"
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readAll(t *testing.T, f *os.File) string {
	t.Helper()
	b, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fixture: root/in is the policy, root/out is not.
func fixture(t *testing.T) (root string, p *policy) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"in", "out"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root, &policy{allow: filepath.Join(root, "in")}
}

func TestOpenRegularFile(t *testing.T) {
	root, p := fixture(t)
	write(t, filepath.Join(root, "in", "a"), "hello")
	f, err := Opener{Gate: p, Cwd: root}.Open(context.Background(), "in/a")
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, f); got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestSymlinkInsidePolicyIsFollowedAndTheTargetIsJudged(t *testing.T) {
	root, p := fixture(t)
	write(t, filepath.Join(root, "in", "real"), "target")
	if err := os.Symlink("real", filepath.Join(root, "in", "link")); err != nil {
		t.Fatal(err)
	}
	f, err := Opener{Gate: p, Cwd: root}.Open(context.Background(), "in/link")
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, f); got != "target" {
		t.Fatalf("got %q", got)
	}
	last := p.asked[len(p.asked)-1]
	if last != filepath.Join(root, "in", "real") {
		t.Fatalf("the gate's last question was %q, want the resolved target", last)
	}
}

func TestSymlinkOutOfPolicyIsRefusedNamingThePath(t *testing.T) {
	root, p := fixture(t)
	write(t, filepath.Join(root, "out", "secret"), "s3")
	if err := os.Symlink(filepath.Join(root, "out", "secret"), filepath.Join(root, "in", "link")); err != nil {
		t.Fatal(err)
	}
	_, err := Opener{Gate: p, Cwd: root}.Open(context.Background(), "in/link")
	if err == nil {
		t.Fatal("followed a symlink out of the policy")
	}
	for _, want := range []string{"in/link", filepath.Join(root, "out", "secret"), "outside the read policy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestSymlinkDirectoryComponentOutOfPolicyIsRefused(t *testing.T) {
	root, p := fixture(t)
	write(t, filepath.Join(root, "out", "secret"), "s3")
	// Swapped after the verdict: judged as root/in/d/secret by name, but d is
	// a link by the time of the open. Judge it the way the open sees it.
	if err := os.Symlink(filepath.Join(root, "out"), filepath.Join(root, "in", "d")); err != nil {
		t.Fatal(err)
	}
	_, err := Opener{Gate: p, Cwd: root}.openThroughLink(context.Background(), "in/d/secret", filepath.Join(root, "in", "d", "secret"))
	if err == nil {
		t.Fatal("followed a swapped directory link out of the policy")
	}
}

func TestAskedTargetIsNotAllowed(t *testing.T) {
	root, p := fixture(t)
	write(t, filepath.Join(root, "out", "x"), "x")
	if err := os.Symlink(filepath.Join(root, "out", "x"), filepath.Join(root, "in", "l")); err != nil {
		t.Fatal(err)
	}
	if _, err := (Opener{Gate: p, Cwd: root}).Open(context.Background(), "in/l"); err == nil {
		t.Fatal("an ask for the target must refuse")
	}
}

func TestDeniedNameIsRefusedBeforeOpening(t *testing.T) {
	root, p := fixture(t)
	p.deny = filepath.Join(root, "in", "no")
	write(t, filepath.Join(root, "in", "no"), "x")
	_, err := Opener{Gate: p, Cwd: root}.Open(context.Background(), "in/no")
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("got %v", err)
	}
}

func TestBrokenLinkAndNonRegularAreRefused(t *testing.T) {
	root, p := fixture(t)
	if err := os.Symlink("nowhere", filepath.Join(root, "in", "dangling")); err != nil {
		t.Fatal(err)
	}
	o := Opener{Gate: p, Cwd: root}
	if _, err := o.Open(context.Background(), "in/dangling"); err == nil {
		t.Fatal("opened a dangling link")
	}
	if _, err := o.Open(context.Background(), "in"); err == nil {
		t.Fatal("Open returned a directory")
	}
	if err := o.Verify(context.Background(), "in"); err != nil {
		t.Fatalf("Verify of a directory inside the policy: %v", err)
	}
}
