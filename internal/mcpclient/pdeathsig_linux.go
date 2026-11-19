//go:build linux

package mcpclient

import (
	"os/exec"
	"runtime"
	"sync"
	"syscall"
)

// A stdio child must not outlive moxy, even when moxy dies without unwinding
// (SIGKILL, OOM; moxy#527): the kernel signals it when its parent dies. SIGTERM,
// not SIGKILL, so a child that runs subprocesses of its own can stop them first.
// A child that ignores SIGTERM and never reads its stdin to EOF still survives.
func setParentDeathSignal(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
}

// Pdeathsig fires when the OS THREAD that forked the child exits, not when the
// process does (golang/go#27505). Go may run any goroutine on any thread, and a
// thread can be retired, so starting a child from an arbitrary goroutine could
// kill it early. Every child is therefore started from one goroutine locked to
// one thread for the life of the process.
var (
	spawnerOnce sync.Once
	spawnerJobs = make(chan func())
)

func startChild(cmd *exec.Cmd) error {
	spawnerOnce.Do(func() {
		go func() {
			runtime.LockOSThread()
			for job := range spawnerJobs {
				job()
			}
		}()
	})
	errc := make(chan error, 1)
	spawnerJobs <- func() { errc <- cmd.Start() }
	return <-errc
}
