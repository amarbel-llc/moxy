package toolbox

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func needPrograms(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := exec.LookPath(n); err != nil {
			t.Skipf("%s not on PATH", n)
		}
	}
}

func allowAll(context.Context, string) error { return nil }

func run(t *testing.T, cwd, command, stdin string, check func(context.Context, string) error) (string, error) {
	t.Helper()
	p, err := Parse(command)
	if err != nil {
		t.Fatalf("Parse(%q): %v", command, err)
	}
	var out, errOut bytes.Buffer
	err = Run(context.Background(), p, RunOptions{
		Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errOut, Cwd: cwd, Check: check,
	})
	if err != nil {
		return out.String(), errors.Join(err, errors.New(errOut.String()))
	}
	return out.String(), nil
}

func TestRunPipeline(t *testing.T) {
	needPrograms(t, "sort", "uniq", "tr")
	got, err := run(t, t.TempDir(), "tr a-z A-Z | sort | uniq -c", "b\na\nb\n", allowAll)
	if err != nil {
		t.Fatal(err)
	}
	if want := "      1 A\n      2 B\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunDigestOfStdin(t *testing.T) {
	got, err := run(t, t.TempDir(), "digest -a sha256", "abc", allowAll)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "sha256-") || !strings.HasSuffix(got, "  -\n") {
		t.Errorf("got %q", got)
	}
}

func TestRunChecksBeforeOpening(t *testing.T) {
	dir := t.TempDir()
	var asked []string
	refuse := func(_ context.Context, path string) error {
		asked = append(asked, path)
		return errors.New("outside the policy")
	}
	_, err := run(t, dir, "digest missing.txt", "", refuse)
	if err == nil || !strings.Contains(err.Error(), "missing.txt: outside the policy") {
		t.Fatalf("err = %v", err)
	}
	if len(asked) != 1 || asked[0] != filepath.Join(dir, "missing.txt") {
		t.Errorf("asked %q", asked)
	}
}

func TestRunRefusesSymlinks(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "leaf")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real", "f"), []byte("f"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, dir, "digest leaf", "", allowAll); err == nil {
		t.Error("a symlink leaf was followed")
	}
	if got, err := run(t, dir, "digest real/f", "", allowAll); err != nil || !strings.Contains(got, "real/f") {
		t.Errorf("a plain file failed: %q %v", got, err)
	}
}

func TestRunRefusesNonRegularFiles(t *testing.T) {
	if _, err := run(t, t.TempDir(), "digest .", "", allowAll); err == nil {
		t.Error("a directory was read")
	}
}

func TestRunRefusesLowDescriptors(t *testing.T) {
	if _, err := run(t, t.TempDir(), "digest /dev/fd/0", "", allowAll); err == nil {
		t.Error("/dev/fd/0 was accepted as an operand")
	}
}
