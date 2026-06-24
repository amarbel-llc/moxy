//go:build !linux

package stderrlog

import "syscall"

// dupOntoStderr redirects fd 2 (stderr) to be a copy of srcFd. On Darwin and
// the BSDs, syscall.Dup2 is the portable spelling (these platforms predate /
// don't expose Dup3 uniformly in the Go syscall package). See dup_linux.go for
// the Linux path.
func dupOntoStderr(srcFd int) error {
	return syscall.Dup2(srcFd, 2)
}
