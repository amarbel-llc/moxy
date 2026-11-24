package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// pluginRuntimeDirEnv names the private directory clown creates for a plugin
// server (clown's plugin listeners contract v1): mode 0700, owned by the
// session's user, never visible to a confined session. Under it serve-http
// listens on a unix socket instead of loopback TCP, and that directory is the
// listener's only protection: moxy holds no token.
const pluginRuntimeDirEnv = "CLOWN_PLUGIN_RUNTIME_DIR"

const pluginSocketName = "server.sock"

// pluginRuntimeDir is the directory serve-http listens in, or "" for any other
// listener: only serve-http without --listen honours the variable (--listen
// wins; the stdio server never listens).
func pluginRuntimeDir(mode transportMode, listenAddr string, getenv func(string) string) string {
	if mode != transportHTTP || listenAddr != "" {
		return ""
	}
	return getenv(pluginRuntimeDirEnv)
}

// pluginSocketPath checks dir against the contract and returns the socket
// path in it. It refuses a directory that is relative, missing, not a
// directory (a symlink included), not owned by euid, or reachable by group or
// world, and a socket path that would not fit in sun_path.
func pluginSocketPath(dir string, euid int) (string, error) {
	refuse := func(format string, a ...any) (string, error) {
		return "", fmt.Errorf("%s=%q: %s", pluginRuntimeDirEnv, dir, fmt.Sprintf(format, a...))
	}
	if !filepath.IsAbs(dir) {
		return refuse("not an absolute path")
	}
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return refuse("does not exist (clown creates it before starting the plugin)")
	}
	if err != nil {
		return refuse("%v", err)
	}
	if !fi.IsDir() {
		return refuse("not a directory (mode %v)", fi.Mode().Type())
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return refuse("cannot read its owner")
	}
	if int(st.Uid) != euid {
		return refuse("owned by uid %d, not by this process's uid %d", st.Uid, euid)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return refuse("mode %04o gives group or world access; want 0700", perm)
	}
	path := filepath.Join(dir, pluginSocketName)
	// sun_path holds the path and its terminating NUL.
	if limit := len(syscall.RawSockaddrUnix{}.Path) - 1; len(path) > limit {
		return refuse("socket path %q is too long (%d bytes, limit %d)", path, len(path), limit)
	}
	return path, nil
}

// listenPluginSocket listens on dir's server.sock with mode 0600 after
// checking dir (pluginSocketPath). A socket left by a predecessor is removed
// first; any other file at that path is a refusal, left as it is. Closing the
// listener removes the socket.
func listenPluginSocket(dir string) (net.Listener, string, error) {
	path, err := pluginSocketPath(dir, os.Geteuid())
	if err != nil {
		return nil, "", err
	}
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode().Type() != fs.ModeSocket {
			return nil, "", fmt.Errorf("%s: %s exists and is not a socket (mode %v); not replacing it", pluginRuntimeDirEnv, path, fi.Mode().Type())
		}
		if err := os.Remove(path); err != nil {
			return nil, "", fmt.Errorf("%s: removing stale socket %s: %w", pluginRuntimeDirEnv, path, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, "", fmt.Errorf("%s: %w", pluginRuntimeDirEnv, err)
	}

	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, "", fmt.Errorf("%s: listening on %s: %w", pluginRuntimeDirEnv, path, err)
	}
	// The directory is already 0700; the socket's own mode is the second
	// fence, set before anything is served on it.
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, "", fmt.Errorf("%s: chmod %s: %w", pluginRuntimeDirEnv, path, err)
	}
	return ln, path, nil
}
