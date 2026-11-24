package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// pluginRuntimeDirEnv names the private directory clown creates for a plugin
// server (clown's plugin listeners contract v1): mode 0700, owned by the
// session's user. Under it serve-http listens on a unix socket instead of
// loopback TCP. The directory and the socket's 0600 mode keep out other
// users, and sessions that cannot see the directory (clown hides it from
// confined ones); they do NOT keep out an unconfined process of the same
// account, which can reach the socket by its path. moxy holds no token.
const pluginRuntimeDirEnv = "CLOWN_PLUGIN_RUNTIME_DIR"

const pluginSocketName = "server.sock"

// staleProbeTimeout bounds the connect that tells a dead socket file from a
// live server before the file is replaced.
const staleProbeTimeout = 200 * time.Millisecond

// takePluginRuntimeDir reads CLOWN_PLUGIN_RUNTIME_DIR and removes it from the
// environment in every mode, used or not, so nothing moxy starts inherits it:
// a nested serve-http would otherwise listen on this moxy's socket path.
func takePluginRuntimeDir(mode transportMode, listenAddr string) (string, error) {
	value, present := os.LookupEnv(pluginRuntimeDirEnv)
	if err := os.Unsetenv(pluginRuntimeDirEnv); err != nil {
		return "", fmt.Errorf("unsetting %s: %w", pluginRuntimeDirEnv, err)
	}
	return selectPluginRuntimeDir(mode, listenAddr, value, present)
}

// selectPluginRuntimeDir is the directory serve-http listens in, or "" for
// any other listener: only serve-http without --listen honours the variable
// (--listen wins; the stdio server never listens). Set but empty is refused
// rather than falling back to unauthenticated TCP.
func selectPluginRuntimeDir(mode transportMode, listenAddr, value string, present bool) (string, error) {
	if mode != transportHTTP || listenAddr != "" || !present {
		return "", nil
	}
	if value == "" {
		return "", fmt.Errorf("%s is set but empty; refusing to fall back to an unauthenticated TCP listener", pluginRuntimeDirEnv)
	}
	return value, nil
}

// pluginSocketPath checks dir against the contract and returns the socket
// path in it. It refuses a directory that is relative, missing, not a
// directory (a symlink included, with or without a trailing slash), not owned
// by euid, or reachable by group or world, and a socket path that would not
// fit in sun_path. Ancestors of dir are not checked.
func pluginSocketPath(dir string, euid int) (string, error) {
	refuse := func(format string, a ...any) (string, error) {
		return "", fmt.Errorf("%s=%q: %s", pluginRuntimeDirEnv, dir, fmt.Sprintf(format, a...))
	}
	if !filepath.IsAbs(dir) {
		return refuse("not an absolute path")
	}
	// A trailing slash would make Lstat follow a symlink in the last
	// component.
	clean := filepath.Clean(dir)
	fi, err := os.Lstat(clean)
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
	path := filepath.Join(clean, pluginSocketName)
	// sun_path holds the path and its terminating NUL.
	if limit := len(syscall.RawSockaddrUnix{}.Path) - 1; len(path) > limit {
		return refuse("socket path %q is too long (%d bytes, limit %d)", path, len(path), limit)
	}
	return path, nil
}

// listenPluginSocket listens on dir's server.sock with mode 0600 after
// checking dir (pluginSocketPath). A socket file nobody answers on is a dead
// predecessor's and is replaced; a live one, or any other kind of file, is a
// refusal and is left as it is. Closing the listener removes the socket only
// if the file at the path is still the one this call created.
func listenPluginSocket(dir string) (net.Listener, string, error) {
	path, err := pluginSocketPath(dir, os.Geteuid())
	if err != nil {
		return nil, "", err
	}
	if err := removeDeadSocket(path); err != nil {
		return nil, "", err
	}

	ul, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, "", fmt.Errorf("%s: listening on %s: %w", pluginRuntimeDirEnv, path, err)
	}
	ul.SetUnlinkOnClose(false)
	// The directory is already 0700; the socket's own mode is the second
	// fence, set before anything is served on it.
	if err := os.Chmod(path, 0o600); err != nil {
		ul.Close()
		os.Remove(path)
		return nil, "", fmt.Errorf("%s: chmod %s: %w", pluginRuntimeDirEnv, path, err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		ul.Close()
		os.Remove(path)
		return nil, "", fmt.Errorf("%s: %w", pluginRuntimeDirEnv, err)
	}
	return &pluginListener{UnixListener: ul, path: path, file: fi}, path, nil
}

// removeDeadSocket clears path for a new listener: nothing there is fine; a
// socket that refuses connections is a dead predecessor's and is removed;
// a socket that answers (or cannot be probed), or any other file, is refused.
func removeDeadSocket(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w", pluginRuntimeDirEnv, err)
	}
	if fi.Mode().Type() != fs.ModeSocket {
		return fmt.Errorf("%s: %s exists and is not a socket (mode %v); not replacing it", pluginRuntimeDirEnv, path, fi.Mode().Type())
	}
	conn, err := net.DialTimeout("unix", path, staleProbeTimeout)
	if err == nil {
		conn.Close()
		return fmt.Errorf("%s: a live server is listening on %s; not replacing it", pluginRuntimeDirEnv, path)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("%s: cannot tell whether %s is live (%v); not replacing it", pluginRuntimeDirEnv, path, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("%s: removing stale socket %s: %w", pluginRuntimeDirEnv, path, err)
	}
	return nil
}

// pluginListener removes its socket file on Close only while the path still
// names the file it bound, so a successor's socket at the same path survives
// this one's shutdown.
type pluginListener struct {
	*net.UnixListener
	path string
	file fs.FileInfo
	once sync.Once
}

// Close checks and removes the file BEFORE closing the listening descriptor:
// while it still listens, a successor's probe connects and refuses to
// replace the file, so the file cannot change between the check and the
// remove.
func (l *pluginListener) Close() error {
	l.once.Do(func() {
		if fi, lerr := os.Lstat(l.path); lerr == nil && os.SameFile(fi, l.file) {
			os.Remove(l.path)
		}
	})
	return l.UnixListener.Close()
}
