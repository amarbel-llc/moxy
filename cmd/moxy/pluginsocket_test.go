package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// privateDir returns a fresh directory with mode 0700, as clown creates
// CLOWN_PLUGIN_RUNTIME_DIR.
func privateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "rt")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPluginSocketPathAcceptsPrivateDir(t *testing.T) {
	dir := privateDir(t)
	got, err := pluginSocketPath(dir, os.Geteuid())
	if err != nil {
		t.Fatalf("pluginSocketPath: %v", err)
	}
	if want := filepath.Join(dir, "server.sock"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestPluginSocketPathRefusals(t *testing.T) {
	euid := os.Geteuid()

	missing := filepath.Join(t.TempDir(), "absent")

	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	target := privateDir(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	groupBits := privateDir(t)
	if err := os.Chmod(groupBits, 0o750); err != nil {
		t.Fatal(err)
	}
	worldBits := privateDir(t)
	if err := os.Chmod(worldBits, 0o701); err != nil {
		t.Fatal(err)
	}

	// A path whose socket would not fit in sun_path: the directory itself is
	// creatable (NAME_MAX is 255), only the socket path is too long.
	long := filepath.Join(t.TempDir(), strings.Repeat("d", 120))
	if err := os.Mkdir(long, 0o700); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		dir  string
		euid int
		want string
	}{
		{"relative", "relative/dir", euid, "not an absolute path"},
		{"missing", missing, euid, "does not exist"},
		{"not a directory", notDir, euid, "not a directory"},
		{"symlink", link, euid, "not a directory"},
		{"group bits", groupBits, euid, "group or world"},
		{"world bits", worldBits, euid, "group or world"},
		{"other owner", privateDir(t), euid + 1, "owned by uid"},
		{"too long", long, euid, "too long"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := pluginSocketPath(c.dir, c.euid)
			if err == nil {
				t.Fatalf("pluginSocketPath(%q) = nil error, want one containing %q", c.dir, c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err, c.want)
			}
			if !strings.Contains(err.Error(), pluginRuntimeDirEnv) {
				t.Fatalf("error %q does not name %s", err, pluginRuntimeDirEnv)
			}
		})
	}
}

func TestListenPluginSocketServesAndUnlinksOnClose(t *testing.T) {
	dir := privateDir(t)
	ln, path, err := listenPluginSocket(dir)
	if err != nil {
		t.Fatalf("listenPluginSocket: %v", err)
	}
	if path != filepath.Join(dir, "server.sock") {
		t.Fatalf("path = %q", path)
	}
	if ln.Addr().Network() != "unix" {
		t.Fatalf("network = %q, want unix", ln.Addr().Network())
	}

	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat socket: %v", err)
	}
	if fi.Mode().Type() != os.ModeSocket {
		t.Fatalf("mode type = %v, want socket", fi.Mode().Type())
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket perm = %o, want 600", perm)
	}

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}}
	resp, err := client.Get("http://moxy/healthz")
	if err != nil {
		t.Fatalf("GET over socket: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	<-done
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket still present after close (lstat err %v)", err)
	}
}

func TestListenPluginSocketReplacesStaleSocket(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "server.sock")
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("stale socket not left behind: %v", err)
	}

	ln, _, err := listenPluginSocket(dir)
	if err != nil {
		t.Fatalf("listenPluginSocket over a stale socket: %v", err)
	}
	ln.Close()
}

func TestListenPluginSocketRefusesNonSocketFile(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "server.sock")
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	ln, _, err := listenPluginSocket(dir)
	if err == nil {
		ln.Close()
		t.Fatal("listenPluginSocket over a regular file: nil error")
	}
	if !strings.Contains(err.Error(), "not a socket") {
		t.Fatalf("error %q does not contain %q", err, "not a socket")
	}
	if b, rerr := os.ReadFile(path); rerr != nil || string(b) != "keep me" {
		t.Fatalf("regular file was touched: %q, %v", b, rerr)
	}
}

func TestPluginRuntimeDirSelection(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == pluginRuntimeDirEnv {
				return v
			}
			return ""
		}
	}
	cases := []struct {
		name   string
		mode   transportMode
		listen string
		value  string
		want   string
	}{
		{"http, variable set: unix socket", transportHTTP, "", "/run/x", "/run/x"},
		{"http, variable unset: tcp fallback", transportHTTP, "", "", ""},
		{"http, --listen wins over the variable", transportHTTP, "127.0.0.1:1", "/run/x", ""},
		{"stdio ignores the variable", transportStdio, "", "/run/x", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pluginRuntimeDir(c.mode, c.listen, env(c.value)); got != c.want {
				t.Fatalf("pluginRuntimeDir = %q, want %q", got, c.want)
			}
		})
	}
}
