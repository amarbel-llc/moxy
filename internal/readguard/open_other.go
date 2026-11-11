//go:build !linux

package readguard

import (
	"os"
	"syscall"
)

// openNoSymlinks opens path read-only without following a symlink leaf; a
// component above the leaf swapped for a link is not detected (no openat2).
func openNoSymlinks(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
