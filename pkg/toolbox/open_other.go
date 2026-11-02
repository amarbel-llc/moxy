//go:build !linux

package toolbox

import (
	"os"
	"syscall"
)

// openNoSymlinks opens path read-only without following a symlink leaf.
// Without openat2 a component above the leaf swapped for a link after the
// verdict is not detected; the gate's own canonicalization resolved those
// components just before.
func openNoSymlinks(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
