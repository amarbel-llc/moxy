package main

import (
	"os"
	"syscall"
)

// kept holds the opened files until the exec replaces the process, so a
// finalizer cannot close a descriptor the program is about to inherit.
var kept []*os.File

// keepAcrossExec clears close-on-exec on f's descriptor so the program
// exec'd next inherits it.
func keepAcrossExec(f *os.File) error {
	kept = append(kept, f)
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_SETFD, 0); errno != 0 {
		return errno
	}
	return nil
}
