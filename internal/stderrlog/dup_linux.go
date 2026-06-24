//go:build linux

package stderrlog

import "syscall"

// dupOntoStderr redirects fd 2 (stderr) to be a copy of srcFd. On Linux,
// syscall.Dup2 is not defined on every architecture (notably arm64, riscv64,
// loong64) — only Dup3 is portable across all Linux arches. Dup3 with flags=0
// is equivalent to Dup2 (POSIX dup2(2)); the third argument carries
// O_CLOEXEC-style flags, of which we want none.
func dupOntoStderr(srcFd int) error {
	return syscall.Dup3(srcFd, 2, 0)
}
