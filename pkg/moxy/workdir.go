package moxy

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// recoverWorkingDirectory moves the process out of a working directory that
// was deleted under it, a subagent removing the scratch dir moxy was started
// in (moxy#439). Every tool moxy runs inherits that directory, so git and
// anything walking from "." fail until the process stands somewhere real.
// It goes to the nearest existing ancestor of startDir (the directory moxy
// was started in, else $PWD), which is the closest stand-in for where the
// session meant to be. A working directory that still exists is left alone.
func recoverWorkingDirectory(startDir string, stderr io.Writer) {
	if _, err := os.Getwd(); err == nil {
		return
	}
	from := startDir
	if from == "" {
		from = os.Getenv("PWD")
	}
	if from == "" {
		from = string(filepath.Separator)
	}
	for dir := filepath.Clean(from); ; dir = filepath.Dir(dir) {
		if err := os.Chdir(dir); err == nil {
			fmt.Fprintf(stderr, "moxy: working directory %s no longer exists; moved to %s\n", from, dir)
			return
		}
		if dir == filepath.Dir(dir) {
			fmt.Fprintf(stderr, "moxy: working directory %s no longer exists, and no ancestor could be entered\n", from)
			return
		}
	}
}
